package dialogue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNativeDecisionRoundTrip(t *testing.T) {
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream     bool      `json:"stream"`
			ToolChoice string    `json:"tool_choice"`
			Tools      []Tool    `json:"tools"`
			Messages   []Message `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
		}
		if req.ToolChoice != "required" {
			t.Error("mandatory tool choice missing")
		}
		if req.Stream || len(req.Tools) != 1 || req.Tools[0].Function.Name != "mcp_1_formula_hint" || r.Header.Get("Authorization") != "Bearer test-gateway-credential" {
			t.Error("native tools/authorization missing")
		}
		if req.Messages[2].Role != "tool" || req.Messages[2].ToolCallID != "call-1" || req.Messages[1].ToolCalls[0].ID != "call-1" {
			t.Error("tool message correlation lost")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call-2","type":"function","function":{"name":"mcp_1_formula_hint","arguments":"{\"query\":\"下一步\"}"}}]}}]}`))
	}))
	defer h.Close()
	call := ToolCall{ID: "call-1", Type: "function", Function: Function{Name: "mcp_1_formula_hint", Arguments: `{"query":"上一问"}`}}
	g := HTTPGateway{BaseURL: h.URL, APIKey: "test-gateway-credential"}
	req := Request{ToolChoice: "required", Tools: []Tool{{Type: "function", Function: Function{Name: "mcp_1_formula_hint", Parameters: json.RawMessage(`{"type":"object"}`)}}}, Messages: []Message{{Role: "user", Content: "问题"}, {Role: "assistant", ToolCalls: []ToolCall{call}}, {Role: "tool", Content: "数据", ToolCallID: "call-1"}}}
	got, e := g.Decide(context.Background(), req)
	if e != nil || len(got.Calls) != 1 || got.Calls[0].ID != "call-2" || got.Calls[0].Function.Arguments != `{"query":"下一步"}` {
		t.Fatalf("decision: %+v %v", got, e)
	}
}
