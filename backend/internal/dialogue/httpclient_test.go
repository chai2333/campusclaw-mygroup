package dialogue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sseBody(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		var chunk chatStreamChunk
		chunk.Choices = append(chunk.Choices, struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		}{Delta: struct {
			Content string `json:"content"`
		}{Content: c}})
		data, _ := json.Marshal(chunk)
		b.WriteString("data: ")
		b.Write(data)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// TestHTTPGatewayStream parses a real chunked SSE response and asserts the
// deltas, accumulation, auth header and model routing.
func TestHTTPGatewayStream(t *testing.T) {
	var gotAuth, gotPath, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		gotModel = req.Model
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for part := range strings.SplitSeq(sseBody("第一段\n", "第二段[1]"), "\n\n") {
			if part == "" {
				continue
			}
			_, _ = w.Write([]byte(part + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	gw := &HTTPGateway{BaseURL: srv.URL, Model: "test-model", APIKey: "sk-secret", Timeout: 5 * time.Second}
	var deltas []string
	full, err := gw.Stream(context.Background(), Request{
		Model:     "",
		MaxTokens: 128,
		Messages:  []Message{{Role: "system", Content: "系统"}, {Role: "user", Content: "问题"}},
	}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if full != "第一段\n第二段[1]" {
		t.Fatalf("accumulated text wrong: %q", full)
	}
	if len(deltas) != 2 || deltas[0] != "第一段\n" {
		t.Fatalf("delta order wrong: %+v", deltas)
	}
	if gotAuth != "Bearer sk-secret" {
		t.Fatalf("auth header %q", gotAuth)
	}
	if gotPath != "/chat/completions" || gotModel != "test-model" {
		t.Fatalf("request wrong: path=%s model=%s", gotPath, gotModel)
	}
	// The sentinel error text never carries provider details.
	if strings.Contains(ErrUnavailable.Error(), "sk-secret") || strings.Contains(ErrUnavailable.Error(), srv.URL) {
		t.Fatal("sentinel error leaks provider details")
	}
}

// TestHTTPGatewayErrors maps provider failures to safe sentinels.
func TestHTTPGatewayErrors(t *testing.T) {
	status := http.StatusBadGateway
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer srv.Close()
	gw := &HTTPGateway{BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second}
	if _, err := gw.Stream(context.Background(), Request{}, nil); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}

	// A provider error payload also maps to ErrUnavailable.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`data: {"error":{"message":"quota exceeded"}}` + "\n\n"))
	}))
	defer errSrv.Close()
	gw2 := &HTTPGateway{BaseURL: errSrv.URL, Model: "m", Timeout: 5 * time.Second}
	if _, err := gw2.Stream(context.Background(), Request{}, nil); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected ErrUnavailable for provider error, got %v", err)
	}
}

// TestHTTPGatewayCancellationAndTimeout verifies ctx cancellation surfaces
// and an exceeded deadline maps to ErrTimeout.
func TestHTTPGatewayCancellationAndTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	gw := &HTTPGateway{BaseURL: srv.URL, Model: "m", Timeout: 10 * time.Second}
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if _, err := gw.Stream(ctx, Request{}, nil); err == nil {
		t.Fatal("expected cancellation error")
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(120 * time.Millisecond)
	}))
	defer slow.Close()
	gw2 := &HTTPGateway{BaseURL: slow.URL, Model: "m", Timeout: 30 * time.Millisecond}
	if _, err := gw2.Stream(context.Background(), Request{}, nil); err != ErrTimeout {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
}
