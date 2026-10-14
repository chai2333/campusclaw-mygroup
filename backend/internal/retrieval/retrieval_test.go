package retrieval_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/db"
	"campusclaw/internal/knowledge"
	"campusclaw/internal/retrieval"
	"campusclaw/internal/retrieval/retrievaltest"
)

func testDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set; skipping MySQL integration test")
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.Wait(ctx, conn); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	if err := db.Migrate(ctx2, conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cleanup(t, conn)
	return conn
}

func cleanup(t *testing.T, conn *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		"SET FOREIGN_KEY_CHECKS=0",
 "TRUNCATE TABLE tutor_agent_materials", "TRUNCATE TABLE tutor_agent_skills", "TRUNCATE TABLE tutor_agents", "TRUNCATE TABLE skills",
		"TRUNCATE TABLE knowledge_chunks",
		"TRUNCATE TABLE knowledge_entries",
		"TRUNCATE TABLE materials",
		"TRUNCATE TABLE sessions",
		"TRUNCATE TABLE users",
		"TRUNCATE TABLE classes",
		"SET FOREIGN_KEY_CHECKS=1",
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("cleanup %q: %v", stmt, err)
		}
	}
}

// fixture creates two classes with one ready material each and returns the
// class IDs and material IDs. Class A mentions 清华园; class B does not.
func fixture(t *testing.T, conn *sql.DB) (classA, classB int64, matA, matB string) {
	t.Helper()
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, "INSERT INTO classes(slug,name) VALUES('ra','检索A班'),('rb','检索B班')"); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT id FROM classes WHERE slug='ra'").Scan(&classA); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT id FROM classes WHERE slug='rb'").Scan(&classB); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"INSERT INTO users(username,password_hash,role,class_id) VALUES('ra-t','$2a$12$xxx','teacher',?),('rb-t','$2a$12$xxx','teacher',?)", classA, classB); err != nil {
		t.Fatal(err)
	}
	var upA, upB int64
	if err := conn.QueryRowContext(ctx, "SELECT id FROM users WHERE username='ra-t'").Scan(&upA); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT id FROM users WHERE username='rb-t'").Scan(&upB); err != nil {
		t.Fatal(err)
	}
	matA, matB = "mat-ra", "mat-rb"
	if _, err := conn.ExecContext(ctx,
		"INSERT INTO materials(id,class_id,uploader_id,title,original_filename,storage_key) VALUES(?,?,?,?,?,?)", matA, classA, upA, "A班讲义", "a.md", "sk-ra"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"INSERT INTO materials(id,class_id,uploader_id,title,original_filename,storage_key) VALUES(?,?,?,?,?,?)", matB, classB, upB, "B班讲义", "b.md", "sk-rb"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"INSERT INTO materials(id,class_id,uploader_id,title,original_filename,storage_key) VALUES(?,?,?,?,?,?)", "mat-ra2", classA, upA, "A班第二讲", "a2.md", "sk-ra2"); err != nil {
		t.Fatal(err)
	}
	bodyA := "清华园位于北京市海淀区，是本课程第一讲的重点介绍对象。\n\n第二段讲述清华园的历史沿革。\n\n第三段补充清华园的今日风貌。"
	bodyB := "未名湖畔的博雅塔是另一所学校 robotics 社团的活动地点。"
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,body_text) VALUES(?,?,?)", matA, classA, bodyA); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,body_text) VALUES(?,?,?)", matB, classB, bodyB); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,body_text) VALUES(?,?,'仅包含占位正文。')", "mat-ra2", classA); err != nil {
		t.Fatal(err)
	}
	return classA, classB, matA, matB
}

func newIndexer(conn *sql.DB, embedder *retrievaltest.FakeEmbedder, vectors *retrievaltest.FakeVectorIndex) *retrieval.Indexer {
	return &retrieval.Indexer{DB: conn, Embedder: embedder, Vectors: vectors, Dimension: 8}
}

func TestIndexerPublishesAndMarksReady(t *testing.T) {
	conn := testDB(t)
	classA, _, matA, _ := fixture(t, conn)
	embedder := retrievaltest.NewFakeEmbedder(8)
	vectors := retrievaltest.NewFakeVectorIndex(8)
	ix := newIndexer(conn, embedder, vectors)
	if err := ix.IndexMaterial(context.Background(), matA, "auto", knowledge.Options{}); err != nil {
		t.Fatal(err)
	}
	var ready, failed int
	if err := conn.QueryRow("SELECT COUNT(*) FROM knowledge_chunks WHERE material_id=? AND index_status='ready'", matA).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT COUNT(*) FROM knowledge_chunks WHERE index_status='failed'").Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if ready == 0 || failed != 0 {
		t.Fatalf("expected ready chunks and no failures, got ready=%d failed=%d", ready, failed)
	}
	if len(vectors.LastPoints) != ready {
		t.Fatalf("expected %d vector points, got %d", ready, len(vectors.LastPoints))
	}
	for _, p := range vectors.LastPoints {
		if p.ClassID != classA || p.MaterialID != matA || p.ID == 0 {
			t.Fatalf("point payload association wrong: %+v", p)
		}
	}
	// Re-indexing replaces chunks without duplicating them.
	before := ready
	if err := ix.IndexMaterial(context.Background(), matA, "auto", knowledge.Options{}); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := conn.QueryRow("SELECT COUNT(*) FROM knowledge_chunks WHERE material_id=?", matA).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("re-index changed chunk count: %d -> %d", before, after)
	}
}

func TestIndexerEmbedFailureMarksFailedKeepsMaterial(t *testing.T) {
	conn := testDB(t)
	_, _, matA, _ := fixture(t, conn)
	embedder := retrievaltest.NewFakeEmbedder(8)
	embedder.Err = errors.New("gateway down")
	vectors := retrievaltest.NewFakeVectorIndex(8)
	ix := newIndexer(conn, embedder, vectors)
	if err := ix.IndexMaterial(context.Background(), matA, "auto", knowledge.Options{}); !errors.Is(err, retrieval.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	var failed int
	if err := conn.QueryRow("SELECT COUNT(*) FROM knowledge_chunks WHERE material_id=? AND index_status='failed'", matA).Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if failed == 0 {
		t.Fatal("expected failed chunks")
	}
	var materials int
	if err := conn.QueryRow("SELECT COUNT(*) FROM materials WHERE id=?", matA).Scan(&materials); err != nil {
		t.Fatal(err)
	}
	if materials != 1 {
		t.Fatal("material must survive embedding failure")
	}
	if len(vectors.LastPoints) != 0 {
		t.Fatal("no partial vectors may be published")
	}
	// Failed chunks are not retrievable even in keyword mode.
	svc := &retrieval.Service{DB: conn, Embedder: embedder, Vectors: vectors}
	hits, err := svc.Retrieve(context.Background(), 1, "清华园", retrieval.ModeKeyword, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("failed chunks must not be retrievable, got %d", len(hits))
	}
}

func TestIndexerDisabledMarksFailed(t *testing.T) {
	conn := testDB(t)
	_, _, matA, _ := fixture(t, conn)
	ix := &retrieval.Indexer{DB: conn}
	if ix.Enabled() {
		t.Fatal("indexer without clients must be disabled")
	}
	if err := ix.IndexMaterial(context.Background(), matA, "auto", knowledge.Options{}); err != nil {
		t.Fatal(err)
	}
	var failed int
	if err := conn.QueryRow("SELECT COUNT(*) FROM knowledge_chunks WHERE material_id=? AND index_status='failed'", matA).Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if failed == 0 {
		t.Fatal("chunks must be failed when the vector pipeline is disabled")
	}
}

func TestIndexerBackfillIdempotent(t *testing.T) {
	conn := testDB(t)
	fixture(t, conn)
	embedder := retrievaltest.NewFakeEmbedder(8)
	vectors := retrievaltest.NewFakeVectorIndex(8)
	ix := newIndexer(conn, embedder, vectors)
	if err := ix.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := conn.QueryRow("SELECT COUNT(DISTINCT material_id) FROM knowledge_chunks WHERE index_status='ready'").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("expected all three materials indexed, got %d", total)
	}
	beforeUpserts := vectors.Upserts
	if err := ix.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	if vectors.Upserts != beforeUpserts {
		t.Fatal("second backfill re-published existing materials")
	}
}

func TestIndexerReconcileRepublishesDrift(t *testing.T) {
	conn := testDB(t)
	_, _, matA, _ := fixture(t, conn)
	embedder := retrievaltest.NewFakeEmbedder(8)
	vectors := retrievaltest.NewFakeVectorIndex(8)
	ix := newIndexer(conn, embedder, vectors)
	if err := ix.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Simulate drift: drop one material's points entirely.
	if err := vectors.DeleteByMaterial(context.Background(), matA); err != nil {
		t.Fatal(err)
	}
	if err := ix.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	ids, err := vectors.IDsByMaterial(context.Background(), matA)
	if err != nil {
		t.Fatal(err)
	}
	var ready int
	if err := conn.QueryRow("SELECT COUNT(*) FROM knowledge_chunks WHERE material_id=? AND index_status='ready'", matA).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if len(ids) != ready || ready == 0 {
		t.Fatalf("reconcile did not restore points: ids=%d ready=%d", len(ids), ready)
	}
}

func TestKeywordClassIsolationAndChineseFulltext(t *testing.T) {
	conn := testDB(t)
	classA, classB, matA, _ := fixture(t, conn)
	embedder := retrievaltest.NewFakeEmbedder(8)
	vectors := retrievaltest.NewFakeVectorIndex(8)
	ix := newIndexer(conn, embedder, vectors)
	if err := ix.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := &retrieval.Service{DB: conn, Embedder: embedder, Vectors: vectors}

	hits, err := svc.Retrieve(context.Background(), classA, "清华园", retrieval.ModeKeyword, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("keyword retrieval should find the Chinese term 清华园")
	}
	for _, h := range hits {
		if h.ClassID != classA {
			t.Fatal("hit from wrong class")
		}
		if h.MaterialID != matA && h.MaterialID != "mat-ra2" {
			t.Fatalf("unexpected material %s", h.MaterialID)
		}
		if h.ChunkText == "" || h.MaterialTitle == "" {
			t.Fatal("hit must carry MySQL chunk text and title")
		}
	}
	// Class B has no 清华园 content: empty hits, no error and no leak.
	bHits, err := svc.Retrieve(context.Background(), classB, "清华园", retrieval.ModeKeyword, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(bHits) != 0 {
		t.Fatalf("cross-class keyword leak: %+v", bHits)
	}
	// Relevance ordering: the first hit contains the term.
	if !strings.Contains(hits[0].ChunkText, "清华园") {
		t.Fatal("first keyword hit should contain the queried term")
	}
}

func TestVectorModeFiltersAndRechecksInMySQL(t *testing.T) {
	conn := testDB(t)
	classA, _, _, _ := fixture(t, conn)
	embedder := retrievaltest.NewFakeEmbedder(8)
	vectors := retrievaltest.NewFakeVectorIndex(8)
	ix := newIndexer(conn, embedder, vectors)
	if err := ix.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := &retrieval.Service{DB: conn, Embedder: embedder, Vectors: vectors}

	hits, err := svc.Retrieve(context.Background(), classA, "清华园", retrieval.ModeVector, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.ClassID != classA {
			t.Fatal("vector hit from wrong class survived MySQL recheck")
		}
		if h.VectorRank == 0 {
			t.Fatal("vector hits must carry their rank")
		}
	}

	// Craft the similarity directly: seed the store's points for class A
	// chunks with vectors whose cosine against the query is below 0.35.
	queryVector := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	embedder.VectorOf = func(text string) []float32 {
		if strings.Contains(text, "量子") {
			return queryVector
		}
		return retrievaltest.DeterministicVectorFor(text, 8)
	}
	var chunkIDs []int64
	rows, err := conn.Query("SELECT id FROM knowledge_chunks WHERE class_id=? AND index_status='ready'", classA)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		chunkIDs = append(chunkIDs, id)
	}
	rows.Close()
	if len(chunkIDs) == 0 {
		t.Fatal("expected ready chunks for class A")
	}
	low := []retrieval.VectorPoint{}
	for _, id := range chunkIDs {
		low = append(low, retrieval.VectorPoint{ID: id, ClassID: classA, MaterialID: "mat-ra", Vector: []float32{0.30, 0.95, 0, 0, 0, 0, 0, 0}})
	}
	vectors.Seed(low)
	empty, err := svc.Retrieve(context.Background(), classA, "完全无关的量子问题", retrieval.ModeVector, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("candidates below 0.35 must be filtered, got %d", len(empty))
	}
	// Positive control: cosine 1.0 for one chunk must surface it.
	high := []retrieval.VectorPoint{{ID: chunkIDs[0], ClassID: classA, MaterialID: "mat-ra", Vector: queryVector}}
	vectors.Seed(high)
	hits2, err := svc.Retrieve(context.Background(), classA, "完全无关的量子问题", retrieval.ModeVector, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits2) != 1 || hits2[0].ChunkID != chunkIDs[0] {
		t.Fatalf("expected the seeded high-similarity chunk, got %+v", hits2)
	}
}

func TestVectorModeDependencyFailure(t *testing.T) {
	conn := testDB(t)
	classA, _, _, _ := fixture(t, conn)
	embedder := retrievaltest.NewFakeEmbedder(8)
	embedder.Err = errors.New("down")
	vectors := retrievaltest.NewFakeVectorIndex(8)
	svc := &retrieval.Service{DB: conn, Embedder: embedder, Vectors: vectors}
	for _, mode := range []string{retrieval.ModeVector, retrieval.ModeHybrid} {
		if _, err := svc.Retrieve(context.Background(), classA, "清华园", mode, 4); !errors.Is(err, retrieval.ErrUnavailable) {
			t.Fatalf("%s must fail with ErrUnavailable, got %v", mode, err)
		}
	}
	// Keyword keeps working when the embedding gateway is down.
	if _, err := svc.Retrieve(context.Background(), classA, "清华园", retrieval.ModeKeyword, 4); err != nil {
		t.Fatalf("keyword must not depend on the embedding gateway: %v", err)
	}
	// Disabled pipeline: vector/hybrid unavailable, keyword fine.
	plain := &retrieval.Service{DB: conn}
	if _, err := plain.Retrieve(context.Background(), classA, "清华园", retrieval.ModeHybrid, 4); !errors.Is(err, retrieval.ErrUnavailable) {
		t.Fatal("hybrid without configuration must be unavailable")
	}
	if _, err := plain.Retrieve(context.Background(), classA, "清华园", retrieval.ModeKeyword, 4); err != nil {
		t.Fatal(err)
	}
}

func TestHybridRRFFusionAndCap(t *testing.T) {
	conn := testDB(t)
	classA, _, _, _ := fixture(t, conn)
	embedder := retrievaltest.NewFakeEmbedder(8)
	vectors := retrievaltest.NewFakeVectorIndex(8)
	ix := newIndexer(conn, embedder, vectors)
	if err := ix.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := &retrieval.Service{DB: conn, Embedder: embedder, Vectors: vectors}
	hits, err := svc.Retrieve(context.Background(), classA, "清华园", retrieval.ModeHybrid, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 4 {
		t.Fatalf("hybrid returned %d hits, cap is 4", len(hits))
	}
	seen := map[int64]bool{}
	for i, h := range hits {
		if seen[h.ChunkID] {
			t.Fatal("duplicate chunk in fused results")
		}
		seen[h.ChunkID] = true
		if h.FinalRank != i+1 {
			t.Fatalf("final rank mismatch at %d", i)
		}
		if h.ClassID != classA {
			t.Fatal("hybrid hit from wrong class")
		}
	}
}

func TestRetrieveRejectsBlankQueryAndBadMode(t *testing.T) {
	conn := testDB(t)
	classA, _, _, _ := fixture(t, conn)
	svc := &retrieval.Service{DB: conn}
	if _, err := svc.Retrieve(context.Background(), classA, "   ", retrieval.ModeKeyword, 4); err == nil {
		t.Fatal("blank query must be rejected")
	}
	if _, err := svc.Retrieve(context.Background(), classA, "词", "semantic", 4); err == nil {
		t.Fatal("unsupported mode must be rejected")
	}
	// Empty mode defaults to hybrid.
	embedder := retrievaltest.NewFakeEmbedder(8)
	vectors := retrievaltest.NewFakeVectorIndex(8)
	svc2 := &retrieval.Service{DB: conn, Embedder: embedder, Vectors: vectors}
	if _, err := svc2.Retrieve(context.Background(), classA, "任意问题", "", 4); err != nil {
		t.Fatalf("empty mode should mean hybrid: %v", err)
	}
}
