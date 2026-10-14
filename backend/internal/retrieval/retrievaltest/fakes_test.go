package retrievaltest

import (
	"context"
	"errors"
	"testing"
	"time"

	"campusclaw/internal/retrieval"
)

func TestFakeRetrieverReturnsHitsAndRecordsCall(t *testing.T) {
	hits := []retrieval.Hit{{ChunkID: 7, MaterialID: "mat-a", MaterialTitle: "标题", ChunkIndex: 2, ChunkText: "正文"}}
	f := &FakeRetriever{Hits: hits}
	got, err := f.Retrieve(context.Background(), 3, "查询词", "hybrid", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ChunkID != 7 {
		t.Fatalf("unexpected hits: %+v", got)
	}
	if f.LastClassID != 3 || f.LastQuery != "查询词" || f.LastMode != "hybrid" || f.LastLimit != 4 {
		t.Fatalf("call not recorded: %+v", f)
	}
}

func TestFakeRetrieverNoHits(t *testing.T) {
	f := &FakeRetriever{}
	got, err := f.Retrieve(context.Background(), 1, "问题", "vector", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no hits, got %+v", got)
	}
}

func TestFakeRetrieverProviderError(t *testing.T) {
	f := &FakeRetriever{Err: retrieval.ErrUnavailable}
	if _, err := f.Retrieve(context.Background(), 1, "q", "keyword", 4); !errors.Is(err, retrieval.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestFakeRetrieverCancellationDuringDelay(t *testing.T) {
	f := &FakeRetriever{Hits: []retrieval.Hit{{ChunkID: 1}}, Delay: 50 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	if _, err := f.Retrieve(ctx, 1, "q", "hybrid", 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestFakeEmbedderDeterministicAndOverride(t *testing.T) {
	f := NewFakeEmbedder(4)
	v1, err := f.Embed(context.Background(), []string{"同一段文本"})
	if err != nil {
		t.Fatal(err)
	}
	f2 := NewFakeEmbedder(4)
	v2, err := f2.Embed(context.Background(), []string{"同一段文本"})
	if err != nil {
		t.Fatal(err)
	}
	if len(v1[0]) != 4 || len(v2[0]) != 4 {
		t.Fatalf("unexpected dimensions: %d/%d", len(v1[0]), len(v2[0]))
	}
	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Fatalf("vectors for identical text differ at %d", i)
		}
	}

	overridden := &FakeEmbedder{Dim: 2, VectorOf: func(string) []float32 { return []float32{1, 0} }}
	v3, err := overridden.Embed(context.Background(), []string{"anything"})
	if err != nil {
		t.Fatal(err)
	}
	if v3[0][0] != 1 || v3[0][1] != 0 {
		t.Fatalf("override not applied: %v", v3[0])
	}

	failing := &FakeEmbedder{Err: errors.New("boom")}
	if _, err := failing.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("expected error")
	}
	if len(failing.Calls) != 1 {
		t.Fatalf("calls not recorded: %d", len(failing.Calls))
	}
}

func TestFakeVectorIndexSearchIsClassScopedAndSorted(t *testing.T) {
	idx := NewFakeVectorIndex(2)
	idx.Seed([]retrieval.VectorPoint{
		{ID: 1, ClassID: 1, MaterialID: "m1", Vector: []float32{1, 0}},
		{ID: 2, ClassID: 1, MaterialID: "m1", Vector: []float32{0.6, 0.8}},
		{ID: 3, ClassID: 2, MaterialID: "m2", Vector: []float32{1, 0}},
	})
	hits, err := idx.Search(context.Background(), 1, []float32{1, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("class 2 point leaked: %+v", hits)
	}
	if hits[0].ChunkID != 1 || hits[0].Score < 0.999 {
		t.Fatalf("unexpected top hit: %+v", hits[0])
	}
	if hits[1].ChunkID != 2 {
		t.Fatalf("unexpected second hit: %+v", hits[1])
	}

	limited, err := idx.Search(context.Background(), 1, []float32{1, 0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].ChunkID != 1 {
		t.Fatalf("limit not applied: %+v", limited)
	}

	if err := idx.DeleteByMaterial(context.Background(), "m1"); err != nil {
		t.Fatal(err)
	}
	ids, err := idx.IDsByMaterial(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("delete did not remove points: %v", ids)
	}
	if ids2, _ := idx.IDsByMaterial(context.Background(), "m2"); len(ids2) != 1 || ids2[0] != 3 {
		t.Fatalf("other class points affected: %v", ids2)
	}

	idx.SearchErr = errors.New("qdrant down")
	if _, err := idx.Search(context.Background(), 1, []float32{1, 0}, 5); err == nil {
		t.Fatal("expected injected search error")
	}
}
