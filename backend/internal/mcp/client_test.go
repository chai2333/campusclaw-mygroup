package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReferencesSchemaAndCipher(t *testing.T) {
	for _, v := range []string{"Formula_hint@campus-tools", "formula-hint@campus-tools", "formula_hint", "x@Other", "x@y@z"} {
		if _, _, ok := ParseRef(v); ok {
			t.Fatalf("invalid ref accepted: %s", v)
		}
	}
	if _, _, ok := ParseRef("formula_hint@campus-tools"); !ok {
		t.Fatal("valid ref rejected")
	}
	schema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":2}},"required":["query"],"additionalProperties":false}`)
	for _, v := range []string{`{}`, `{"query":3}`, `{"query":"x"}`, `{"query":"xy","extra":1}`, `[]`} {
		if ValidateArguments(schema, v) == nil {
			t.Fatalf("invalid args accepted: %s", v)
		}
	}
	if ValidateArguments(schema, `{"query":"xy"}`) != nil {
		t.Fatal("valid args rejected")
	}
	if _, e := compileSchema(json.RawMessage(`{"type":"object","$ref":"file:///etc/passwd"}`)); e == nil {
		t.Fatal("external schema load allowed")
	}
	s := Service{Secret: "local-test-key"}
	a, _ := s.encrypt("test-credential-12345")
	b, _ := s.encrypt("test-credential-12345")
	if a == b || strings.Contains(a, "credential") {
		t.Fatal("encryption nonce/boundary")
	}
	raw, e := s.decrypt(a)
	if e != nil || raw != "test-credential-12345" {
		t.Fatal("decrypt")
	}
	s.Secret = "changed"
	if _, e = s.decrypt(a); e == nil {
		t.Fatal("wrong key accepted")
	}
	for _, u := range []string{"file:///tmp/foo", "http://user:pass@localhost", "https://example.com?a=b", "https://example.com/#x"} {
		if _, e := canonical(u); e == nil {
			t.Fatalf("unsafe URL accepted: %s", u)
		}
	}
}
func TestRPCRejectsMismatchedIDsAndRedirects(t *testing.T) {
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; w.Write([]byte(`{}`)) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer source.Close()
	var result any
	if (Client{}).rpc(context.Background(), source.URL, "credential", "tools/list", map[string]any{}, &result) == nil || targetCalls != 0 {
		t.Fatal("credential-bearing redirect followed")
	}
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`)) }))
	defer wrong.Close()
	if (Client{}).rpc(context.Background(), wrong.URL, "credential", "tools/list", map[string]any{}, &result) == nil {
		t.Fatal("mismatched RPC id accepted")
	}
}
