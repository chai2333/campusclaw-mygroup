package retrieval_test

import (
	"campusclaw/internal/retrieval"
	"campusclaw/internal/retrieval/retrievaltest"
	"context"
	"testing"
)

func TestBoundMaterialScopeAtKeywordAndVectorSource(t *testing.T) {
	conn := testDB(t)
	classA, _, matA, _ := fixture(t, conn)
	embedder := retrievaltest.NewFakeEmbedder(8)
	vectors := retrievaltest.NewFakeVectorIndex(8)
	ix := newIndexer(conn, embedder, vectors)
	ctx := context.Background()
	if err := ix.Backfill(ctx); err != nil {
		t.Fatal(err)
	}
	// Give both sources the query vector, so post-limit filtering would be unsafe.
	query := "清华园"
	q, _ := embedder.Embed(ctx, []string{query})
	rows, err := conn.Query("SELECT id,material_id FROM knowledge_chunks WHERE class_id=?", classA)
	if err != nil {
		t.Fatal(err)
	}
	points := []retrieval.VectorPoint{}
	for rows.Next() {
		var id int64
		var material string
		if err := rows.Scan(&id, &material); err != nil {
			t.Fatal(err)
		}
		points = append(points, retrieval.VectorPoint{ID: id, ClassID: classA, MaterialID: material, Vector: q[0]})
	}
	rows.Close()
	vectors.Seed(points)
	svc := &retrieval.Service{DB: conn, Embedder: embedder, Vectors: vectors}
	for _, mode := range []string{retrieval.ModeKeyword, retrieval.ModeVector, retrieval.ModeHybrid} {
		hits, err := svc.RetrieveBound(ctx, classA, query, mode, 1, []string{matA})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 || hits[0].MaterialID != matA {
			t.Fatalf("%s escaped material scope: %+v", mode, hits)
		}
	}
	calls := len(embedder.Calls)
	empty, err := svc.RetrieveBound(ctx, classA, query, retrieval.ModeHybrid, 4, []string{})
	if err != nil || len(empty) != 0 || len(embedder.Calls) != calls {
		t.Fatal("empty binding called external retrieval")
	}
}
