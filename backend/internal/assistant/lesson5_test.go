package assistant

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStrictBodyRejectsTrailingJSONOnce(t *testing.T) {
	for _, body := range []string{`{"enabled":"yes"}`, `{"enabled":true} {}`, `{"enabled":true,"class_id":2}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/", strings.NewReader(body))
		var input struct {
			Enabled *bool `json:"enabled"`
		}
		if decodeStrict(w, r, &input) || w.Code != 400 {
			t.Fatalf("accepted %s", body)
		}
		decoder := json.NewDecoder(w.Body)
		var errorBody map[string]string
		if err := decoder.Decode(&errorBody); err != nil || errorBody["error"] != "invalid_request_body" {
			t.Fatal("invalid error JSON")
		}
		if decoder.Decode(&errorBody) != io.EOF {
			t.Fatal("multiple JSON errors")
		}
	}
}
