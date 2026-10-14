package retrieval

import (
	"context"
	"database/sql"
	"errors"

	"campusclaw/internal/knowledge"
)

// Indexer splits material bodies into chunks, persists them with an index
// status and publishes embeddings to the vector store. A material is never
// rolled back because of embedding failures: its chunks simply end 'failed',
// and only 'ready' chunks are ever retrievable.
type Indexer struct {
	DB        *sql.DB
	Embedder  Embedder    // nil when embedding is not configured
	Vectors   VectorIndex // nil when the vector store is not configured
	Dimension int
}

// Enabled reports whether embedding and vector storage are both configured.
func (ix *Indexer) Enabled() bool {
	return ix != nil && ix.Embedder != nil && ix.Vectors != nil && ix.Dimension > 0
}

// IndexMaterial replaces the material's chunks using the given strategy and,
// when enabled, publishes their vectors. The strategy must be valid even
// when the vector pipeline is disabled, so rebuild requests fail fast.
func (ix *Indexer) IndexMaterial(ctx context.Context, materialID, strategy string, opts knowledge.Options) error {
	var classID int64
	var body string
	err := ix.DB.QueryRowContext(ctx,
		`SELECT e.class_id, e.body_text FROM knowledge_entries e JOIN materials m ON m.id=e.material_id AND m.class_id=e.class_id WHERE e.material_id=? AND m.status='ready'`,
		materialID).Scan(&classID, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("material_not_indexable")
	}
	if err != nil {
		return err
	}
	chunks, err := knowledge.Chunks(body, strategy, opts)
	if err != nil {
		return err
	}
	tx, err := ix.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM knowledge_chunks WHERE material_id=?", materialID); err != nil {
		return err
	}
	for _, c := range chunks {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO knowledge_chunks(material_id,class_id,chunk_index,chunk_text,char_start,char_end,strategy,index_status) VALUES(?,?,?,?,?,?,?,'pending')",
			materialID, classID, c.Index, c.Text, c.CharStart, c.CharEnd, strategy); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if !ix.Enabled() {
		if _, err := ix.DB.ExecContext(ctx, "UPDATE knowledge_chunks SET index_status='failed' WHERE material_id=?", materialID); err != nil {
			return err
		}
		return nil
	}
	return ix.publish(ctx, materialID, classID)
}

// publish embeds all pending chunks of the material and marks them ready.
// Any failure marks the chunks failed and leaves no partial vectors.
func (ix *Indexer) publish(ctx context.Context, materialID string, classID int64) error {
	type row struct {
		id    int64
		index int
		text  string
	}
	rows, err := ix.DB.QueryContext(ctx,
		"SELECT id, chunk_index, chunk_text FROM knowledge_chunks WHERE material_id=? AND index_status='pending' ORDER BY chunk_index", materialID)
	if err != nil {
		return err
	}
	pending := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.index, &r.text); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(pending) == 0 {
		return nil
	}
	fail := func() error {
		_, e := ix.DB.ExecContext(ctx, "UPDATE knowledge_chunks SET index_status='failed' WHERE material_id=?", materialID)
		if e != nil {
			return e
		}
		return ErrUnavailable
	}
	texts := make([]string, len(pending))
	for i, r := range pending {
		texts[i] = r.text
	}
	vectors, err := ix.Embedder.Embed(ctx, texts)
	if err != nil {
		return fail()
	}
	if len(vectors) != len(pending) {
		return fail()
	}
	if err := ix.Vectors.EnsureCollection(ctx, ix.Dimension); err != nil {
		return fail()
	}
	// Replace any previous points for this material before publishing new ones.
	if err := ix.Vectors.DeleteByMaterial(ctx, materialID); err != nil {
		return fail()
	}
	points := make([]VectorPoint, len(pending))
	for i, r := range pending {
		points[i] = VectorPoint{ID: r.id, ClassID: classID, MaterialID: materialID, ChunkIndex: r.index, Vector: vectors[i]}
	}
	if err := ix.Vectors.Upsert(ctx, points); err != nil {
		return fail()
	}
	if _, err := ix.DB.ExecContext(ctx, "UPDATE knowledge_chunks SET index_status='ready' WHERE material_id=?", materialID); err != nil {
		return err
	}
	return nil
}

// Backfill auto-indexes ready materials that have no chunks yet, which keeps
// seed and migrated materials retrievable. Repeated runs do nothing for
// materials that already have chunks.
func (ix *Indexer) Backfill(ctx context.Context) error {
	rows, err := ix.DB.QueryContext(ctx,
		`SELECT e.material_id FROM knowledge_entries e JOIN materials m ON m.id=e.material_id AND m.class_id=e.class_id
		 WHERE m.status='ready' AND NOT EXISTS (SELECT 1 FROM knowledge_chunks c WHERE c.material_id=e.material_id)`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		if err := ix.IndexMaterial(ctx, id, "auto", knowledge.Options{}); err != nil {
			return err
		}
	}
	return nil
}

// Reconcile aligns the vector store with MySQL for every indexed material:
// orphaned or stale points are replaced and missing points are re-embedded.
func (ix *Indexer) Reconcile(ctx context.Context) error {
	if !ix.Enabled() {
		return nil
	}
	rows, err := ix.DB.QueryContext(ctx,
		`SELECT c.material_id, e.class_id FROM knowledge_chunks c
		 JOIN knowledge_entries e ON e.material_id=c.material_id
		 GROUP BY c.material_id, e.class_id`)
	if err != nil {
		return err
	}
	type target struct {
		id      string
		classID int64
	}
	targets := []target{}
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.classID); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, t := range targets {
		ready, err := ix.readyChunkIDs(ctx, t.id)
		if err != nil {
			return err
		}
		published, err := ix.Vectors.IDsByMaterial(ctx, t.id)
		if err != nil {
			return err
		}
		if sameIDs(ready, published) {
			continue
		}
		// Re-publish the material: delete stale points and upsert from MySQL.
		if err := ix.Vectors.DeleteByMaterial(ctx, t.id); err != nil {
			return err
		}
		if _, err := ix.DB.ExecContext(ctx,
			"UPDATE knowledge_chunks SET index_status='pending' WHERE material_id=? AND index_status='ready'", t.id); err != nil {
			return err
		}
		if err := ix.publish(ctx, t.id, t.classID); err != nil && !errors.Is(err, ErrUnavailable) {
			return err
		}
	}
	return nil
}

func (ix *Indexer) readyChunkIDs(ctx context.Context, materialID string) ([]int64, error) {
	rows, err := ix.DB.QueryContext(ctx,
		"SELECT id FROM knowledge_chunks WHERE material_id=? AND index_status='ready' ORDER BY id", materialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
