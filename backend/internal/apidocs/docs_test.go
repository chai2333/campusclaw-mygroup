package apidocs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicDocsAndBearerContract(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)
	for _, path := range []string{"/openapi.json", "/swagger/", "/swagger/assets/swagger-ui-bundle.js", "/swagger/assets/swagger-ui.css"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d empty=%v", path, w.Code, w.Body.Len() == 0)
		}
	}
	data, _ := files.ReadFile("openapi.json")
	var doc struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			Responses map[string]any `json:"responses"`
			Security  []any          `json:"security"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]struct {
				Type   string `json:"type"`
				Scheme string `json:"scheme"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.0.3" || doc.Components.SecuritySchemes["bearerAuth"].Scheme != "bearer" {
		t.Fatal("missing OpenAPI Bearer contract")
	}
	if len(doc.Paths["/api/login"]["post"].Security) != 0 {
		t.Fatal("login must be public")
	}
	for path, methods := range doc.Paths {
		for method, op := range methods {
			if strings.HasPrefix(path, "/api/") && path != "/api/login" {
				if _, ok := op.Responses["401"]; !ok {
					t.Fatalf("%s %s misses 401", method, path)
				}
			}
		}
	}
	if !strings.Contains(string(data), "text/event-stream") || !strings.Contains(string(data), "x-sse-events") {
		t.Fatal("missing SSE contract")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/swagger/unknown", nil))
	if w.Code != 404 {
		t.Fatalf("unknown docs page should 404, got %d", w.Code)
	}
}
