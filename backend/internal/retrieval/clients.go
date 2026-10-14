// Package retrieval indexes knowledge chunks into a rebuildable vector store
// and serves class-scoped keyword, vector and hybrid retrieval. MySQL is
// authoritative: only ready chunks of the caller-supplied class are ever
// returned, and chunk text is always read back from MySQL.
package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Embedder produces fixed-dimension vectors for a batch of texts.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// VectorPoint is one vector record. The payload only carries association IDs,
// never chunk text.
type VectorPoint struct {
	ID         int64
	ClassID    int64
	MaterialID string
	ChunkIndex int
	Vector     []float32
}

// VectorHit is one vector search result; Score is cosine similarity.
type VectorHit struct {
	ChunkID int64
	Score   float32
}

// VectorIndex is the rebuildable vector store. Implementations must scope
// every operation to the current collection.
type VectorIndex interface {
	EnsureCollection(ctx context.Context, dimension int) error
	Upsert(ctx context.Context, points []VectorPoint) error
	DeleteByMaterial(ctx context.Context, materialID string) error
	Search(ctx context.Context, classID int64, vector []float32, limit int) ([]VectorHit, error)
	IDsByMaterial(ctx context.Context, materialID string) ([]int64, error)
}

// HTTPEmbedder calls an OpenAI-compatible POST /embeddings endpoint.
type HTTPEmbedder struct {
	BaseURL   string
	Model     string
	APIKey    string
	Dimension int
	HTTP      *http.Client
}

type embeddingsResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (e *HTTPEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	client := e.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	payload, err := json.Marshal(map[string]any{"model": e.Model, "input": texts})
	if err != nil {
		return nil, errors.New("embedding request invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.BaseURL, "/")+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("embedding request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("embedding gateway unavailable")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<22))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("embedding gateway failed")
	}
	var parsed embeddingsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.New("embedding gateway response invalid")
	}
	if len(parsed.Data) != len(texts) {
		return nil, errors.New("embedding gateway response incomplete")
	}
	out := make([][]float32, len(texts))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(out) {
			return nil, errors.New("embedding gateway response invalid")
		}
		if len(item.Embedding) != e.Dimension {
			return nil, fmt.Errorf("embedding dimension mismatch")
		}
		out[item.Index] = item.Embedding
	}
	for _, v := range out {
		if v == nil {
			return nil, errors.New("embedding gateway response incomplete")
		}
	}
	return out, nil
}

// QdrantIndex talks to a Qdrant collection with cosine distance. Point IDs
// equal knowledge_chunks IDs; payloads carry only association IDs.
type QdrantIndex struct {
	BaseURL    string
	Collection string
	APIKey     string
	HTTP       *http.Client
}

func (q *QdrantIndex) do(ctx context.Context, method, path string, payload any) ([]byte, error) {
	client := q.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.New("vector request invalid")
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(q.BaseURL, "/")+path, reader)
	if err != nil {
		return nil, errors.New("vector request invalid")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if q.APIKey != "" {
		req.Header.Set("api-key", q.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("vector service unavailable")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errors.New("vector service failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The body is returned so callers can inspect provider-specific
		// error payloads (e.g. idempotent re-creation).
		return body, errors.New("vector service failed")
	}
	return body, nil
}

func (q *QdrantIndex) EnsureCollection(ctx context.Context, dimension int) error {
	body, err := q.do(ctx, http.MethodGet, "/collections/"+q.Collection, nil)
	if err == nil {
		var current struct {
			Result struct {
				Config struct {
					Params struct {
						Vectors struct {
							Size     int    `json:"size"`
							Distance string `json:"distance"`
						} `json:"vectors"`
					} `json:"params"`
				} `json:"config"`
			} `json:"result"`
		}
		if json.Unmarshal(body, &current) != nil || current.Result.Config.Params.Vectors.Size != dimension || !strings.EqualFold(current.Result.Config.Params.Vectors.Distance, "Cosine") {
			return errors.New("vector collection incompatible")
		}
		return nil
	}
	lower := strings.ToLower(string(body))
	if !strings.Contains(lower, "not found") && !strings.Contains(lower, "doesn't exist") && !strings.Contains(lower, "does not exist") {
		return err
	}
	_, err = q.do(ctx, http.MethodPut, "/collections/"+q.Collection, map[string]any{
		"vectors": map[string]any{"size": dimension, "distance": "Cosine"},
	})
	return err
}

func (q *QdrantIndex) Upsert(ctx context.Context, points []VectorPoint) error {
	if len(points) == 0 {
		return nil
	}
	docs := make([]map[string]any, len(points))
	for i, p := range points {
		docs[i] = map[string]any{
			"id":     p.ID,
			"vector": p.Vector,
			"payload": map[string]any{
				"class_id":           p.ClassID,
				"material_id":        p.MaterialID,
				"knowledge_entry_id": p.MaterialID,
				"chunk_id":           p.ID,
				"chunk_index":        p.ChunkIndex,
			},
		}
	}
	_, err := q.do(ctx, http.MethodPut, "/collections/"+q.Collection+"/points?wait=true", map[string]any{"points": docs})
	return err
}

func (q *QdrantIndex) DeleteByMaterial(ctx context.Context, materialID string) error {
	_, err := q.do(ctx, http.MethodPost, "/collections/"+q.Collection+"/points/delete", map[string]any{
		"filter": map[string]any{"must": []any{map[string]any{"key": "material_id", "match": map[string]any{"value": materialID}}}},
	})
	return err
}

type qdrantSearchResponse struct {
	Result []struct {
		ID    int64   `json:"id"`
		Score float32 `json:"score"`
	} `json:"result"`
}

func (q *QdrantIndex) Search(ctx context.Context, classID int64, vector []float32, limit int) ([]VectorHit, error) {
	return q.SearchBound(ctx, classID, vector, limit, nil)
}
func (q *QdrantIndex) SearchBound(ctx context.Context, classID int64, vector []float32, limit int, materials []string) ([]VectorHit, error) {
	must := []any{map[string]any{"key": "class_id", "match": map[string]any{"value": classID}}}
	if materials != nil {
		if len(materials) == 0 {
			return []VectorHit{}, nil
		}
		must = append(must, map[string]any{"key": "material_id", "match": map[string]any{"any": materials}})
	}
	body, err := q.do(ctx, http.MethodPost, "/collections/"+q.Collection+"/points/search", map[string]any{
		"vector":       vector,
		"limit":        limit,
		"with_payload": false,
		"filter":       map[string]any{"must": must},
	})
	if err != nil {
		return nil, err
	}
	var parsed qdrantSearchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.New("vector service response invalid")
	}
	hits := make([]VectorHit, len(parsed.Result))
	for i, r := range parsed.Result {
		hits[i] = VectorHit{ChunkID: r.ID, Score: r.Score}
	}
	return hits, nil
}

type qdrantScrollResponse struct {
	Result struct {
		Points []struct {
			ID int64 `json:"id"`
		} `json:"points"`
	} `json:"result"`
}

func (q *QdrantIndex) IDsByMaterial(ctx context.Context, materialID string) ([]int64, error) {
	body, err := q.do(ctx, http.MethodPost, "/collections/"+q.Collection+"/points/scroll", map[string]any{
		"limit":        10000,
		"with_payload": false,
		"with_vector":  false,
		"filter":       map[string]any{"must": []any{map[string]any{"key": "material_id", "match": map[string]any{"value": materialID}}}},
	})
	if err != nil {
		return nil, err
	}
	var parsed qdrantScrollResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.New("vector service response invalid")
	}
	ids := make([]int64, 0, len(parsed.Result.Points))
	for _, p := range parsed.Result.Points {
		ids = append(ids, p.ID)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	return ids, nil
}
