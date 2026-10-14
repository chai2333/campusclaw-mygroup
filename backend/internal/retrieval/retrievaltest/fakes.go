// Package retrievaltest provides deterministic in-memory fakes for the
// retrieval interfaces, used by indexer, search and conversation tests so no
// external network access is required.
package retrievaltest

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"sort"
	"time"

	"campusclaw/internal/retrieval"
)

// FakeEmbedder returns deterministic vectors derived from the text content.
type FakeEmbedder struct {
	Dim      int
	Err      error
	Calls    [][]string
	VectorOf func(text string) []float32 // optional override
}

func NewFakeEmbedder(dim int) *FakeEmbedder {
	return &FakeEmbedder{Dim: dim}
}

func (f *FakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.Calls = append(f.Calls, texts)
	if f.Err != nil {
		return nil, f.Err
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if f.VectorOf != nil {
			out[i] = f.VectorOf(text)
			continue
		}
		out[i] = deterministicVector(text, f.Dim)
	}
	return out, nil
}

func deterministicVector(text string, dim int) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	seed := h.Sum64()
	v := make([]float32, dim)
	for i := range v {
		seed = seed*6364136223846793005 + 1442695040888963407
		v[i] = float32(int64(seed>>33)%2000-1000) / 1000
	}
	return v
}

// DeterministicVectorFor exposes the deterministic vector function to tests.
func DeterministicVectorFor(text string, dim int) []float32 {
	return deterministicVector(text, dim)
}

// FakeVectorIndex is an in-memory vector store with error injection.
type FakeVectorIndex struct {
	points      map[int64]retrieval.VectorPoint
	Dimension   int
	EnsureErr   error
	UpsertErr   error
	DeleteErr   error
	SearchErr   error
	EnsureCalls int
	Upserts     int
	Deletes     int
	LastPoints  []retrieval.VectorPoint
}

func NewFakeVectorIndex(dim int) *FakeVectorIndex {
	return &FakeVectorIndex{points: map[int64]retrieval.VectorPoint{}, Dimension: dim}
}

func (f *FakeVectorIndex) EnsureCollection(ctx context.Context, dimension int) error {
	f.EnsureCalls++
	if dimension != f.Dimension {
		return errors.New("dimension mismatch")
	}
	return f.EnsureErr
}

func (f *FakeVectorIndex) Upsert(ctx context.Context, points []retrieval.VectorPoint) error {
	f.Upserts++
	if f.UpsertErr != nil {
		return f.UpsertErr
	}
	f.LastPoints = points
	for _, p := range points {
		f.points[p.ID] = p
	}
	return nil
}

func (f *FakeVectorIndex) DeleteByMaterial(ctx context.Context, materialID string) error {
	f.Deletes++
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	for id, p := range f.points {
		if p.MaterialID == materialID {
			delete(f.points, id)
		}
	}
	return nil
}

func (f *FakeVectorIndex) Search(ctx context.Context, classID int64, vector []float32, limit int) ([]retrieval.VectorHit, error) {
	if f.SearchErr != nil {
		return nil, f.SearchErr
	}
	hits := []retrieval.VectorHit{}
	for _, p := range f.points {
		if p.ClassID != classID {
			continue
		}
		hits = append(hits, retrieval.VectorHit{ChunkID: p.ID, Score: cosine(vector, p.Vector)})
	}
	sort.Slice(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (f *FakeVectorIndex) IDsByMaterial(ctx context.Context, materialID string) ([]int64, error) {
	ids := []int64{}
	for id, p := range f.points {
		if p.MaterialID == materialID {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	return ids, nil
}

// Seed pre-populates the store so tests can simulate stale or cross-class
// points without running the indexer.
func (f *FakeVectorIndex) Seed(points []retrieval.VectorPoint) {
	for _, p := range points {
		f.points[p.ID] = p
	}
}

func cosine(a, b []float32) float32 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// FakeRetriever records Retrieve calls and returns preset results.
type FakeRetriever struct {
	Hits        []retrieval.Hit
	Err         error
	Delay       time.Duration // simulated latency before responding
	Calls       int
	LastClassID int64
	LastQuery   string
	LastMode    string
	LastLimit   int
}

func (f *FakeRetriever) Retrieve(ctx context.Context, classID int64, query, mode string, limit int) ([]retrieval.Hit, error) {
	f.Calls++
	f.LastClassID = classID
	f.LastQuery = query
	f.LastMode = mode
	f.LastLimit = limit
	if f.Delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.Delay):
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Hits, nil
}

func (f *FakeRetriever) RetrieveBound(ctx context.Context, classID int64, query, mode string, limit int, materials []string) ([]retrieval.Hit, error) {
	hits, err := f.Retrieve(ctx, classID, query, mode, limit)
	allowed := map[string]bool{}
	for _, m := range materials {
		allowed[m] = true
	}
	out := []retrieval.Hit{}
	for _, h := range hits {
		if allowed[h.MaterialID] && h.ClassID == classID {
			out = append(out, h)
		}
	}
	return out, err
}
func (f *FakeVectorIndex) SearchBound(ctx context.Context, classID int64, vector []float32, limit int, materials []string) ([]retrieval.VectorHit, error) {
	if f.SearchErr != nil {
		return nil, f.SearchErr
	}
	allowed := map[string]bool{}
	for _, m := range materials {
		allowed[m] = true
	}
	hits := []retrieval.VectorHit{}
	for _, p := range f.points {
		if p.ClassID == classID && allowed[p.MaterialID] {
			hits = append(hits, retrieval.VectorHit{ChunkID: p.ID, Score: cosine(vector, p.Vector)})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}
