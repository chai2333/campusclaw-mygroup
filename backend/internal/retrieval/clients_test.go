package retrieval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQdrantCollectionCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, distance string
		size           int
		wantErr        bool
	}{{"compatible", "Cosine", 8, false}, {"wrong dimension", "Cosine", 16, true}, {"wrong distance", "Dot", 8, true}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"config": map[string]any{"params": map[string]any{"vectors": map[string]any{"size": tc.size, "distance": tc.distance}}}}})
			}))
			defer srv.Close()
			q := &QdrantIndex{BaseURL: srv.URL, Collection: "chunks", HTTP: srv.Client()}
			err := q.EnsureCollection(context.Background(), 8)
			if (err != nil) != tc.wantErr {
				t.Fatalf("got error %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestQdrantUpsertPayloadContainsIdentifiersOnly(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	q := &QdrantIndex{BaseURL: srv.URL, Collection: "chunks", HTTP: srv.Client()}
	err := q.Upsert(context.Background(), []VectorPoint{{ID: 9, ClassID: 2, MaterialID: "m", ChunkIndex: 3, Vector: []float32{1, 0}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	text := string(raw)
	for _, want := range []string{`"id":9`, `"chunk_id":9`, `"class_id":2`, `"material_id":"m"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %s: %s", want, text)
		}
	}
	for _, forbidden := range []string{"chunk_text", "body_text", "正文"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("payload leaked %s: %s", forbidden, text)
		}
	}
}
