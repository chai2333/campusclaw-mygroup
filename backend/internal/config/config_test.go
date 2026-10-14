package config

import (
	"strings"
	"testing"
)

func valid() map[string]string {
	return map[string]string{"SESSION_SECRET": "test-secret-do-not-deploy", "DB_HOST": "db", "DB_NAME": "campusclaw", "DB_USER": "campusclaw", "DB_PASSWORD": "test-database-password", "UPLOAD_DIR": "/data/uploads", "SEED_TEACHER_PASSWORD": "teacher-fixture", "SEED_STUDENT_PASSWORD": "student-fixture", "DIALOGUE_BASE_URL": "https://gateway.example.internal/v1", "DIALOGUE_MODEL": "test-model", "DIALOGUE_API_KEY": "test-model-key"}
}
func TestRequiredSecretsAndFields(t *testing.T) {
	for _, name := range []string{"SESSION_SECRET", "DB_HOST", "DB_NAME", "DB_USER", "DB_PASSWORD", "UPLOAD_DIR", "SEED_TEACHER_PASSWORD", "SEED_STUDENT_PASSWORD"} {
		t.Run(name, func(t *testing.T) {
			env := valid()
			delete(env, name)
			_, err := Parse(func(k string) string { return env[k] })
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatal("missing field accepted")
			}
			for _, secret := range []string{env["SESSION_SECRET"], env["DB_PASSWORD"], env["SEED_TEACHER_PASSWORD"]} {
				if secret != "" && strings.Contains(err.Error(), secret) {
					t.Fatal("secret in configuration error")
				}
			}
		})
	}
}

func TestDialogueGatewayIsOptionalButAllOrNothing(t *testing.T) {
	off := valid()
	delete(off, "DIALOGUE_BASE_URL")
	delete(off, "DIALOGUE_MODEL")
	delete(off, "DIALOGUE_API_KEY")
	c, err := Parse(func(k string) string { return off[k] })
	if err != nil || c.DialogueEnabled() {
		t.Fatalf("disabled dialogue gateway rejected: %+v, %v", c, err)
	}
	for _, missing := range []string{"DIALOGUE_BASE_URL", "DIALOGUE_MODEL", "DIALOGUE_API_KEY"} {
		partial := valid()
		delete(partial, missing)
		if _, err := Parse(func(k string) string { return partial[k] }); err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("partial dialogue configuration accepted (missing %s)", missing)
		}
	}
}
func TestNumericSettings(t *testing.T) {
	for _, name := range []string{"DB_PORT", "MAX_UPLOAD_BYTES", "SESSION_TTL_SECONDS", "LOGIN_MAX_FAILURES", "LOGIN_WINDOW_SECONDS", "LOGIN_LOCK_SECONDS"} {
		for _, value := range []string{"0", "-1", "not-a-number", "999999999999999999999999"} {
			env := valid()
			env[name] = value
			if _, err := Parse(func(k string) string { return env[k] }); err == nil {
				t.Errorf("accepted invalid %s", name)
			}
		}
	}
}
func TestPasswordByteBoundary(t *testing.T) {
	for _, name := range []string{"SEED_TEACHER_PASSWORD", "SEED_STUDENT_PASSWORD"} {
		env := valid()
		env[name] = strings.Repeat("字", 24)
		if _, err := Parse(func(k string) string { return env[k] }); err != nil {
			t.Fatal(err)
		}
		env[name] += "x"
		if _, err := Parse(func(k string) string { return env[k] }); err == nil {
			t.Fatal("would truncate bcrypt password")
		}
	}
}
func TestDefaultsAndProxyValidation(t *testing.T) {
	env := valid()
	c, err := Parse(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.DBPort != 3306 || c.SessionTTL != 28800 || c.MaxFailures != 5 || len(c.TrustedProxies) != 0 {
		t.Fatal("incorrect defaults")
	}
	env["TRUSTED_PROXY_CIDRS"] = "127.0.0.1/32,::1/128"
	if _, err := Parse(func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
	env["TRUSTED_PROXY_CIDRS"] = "untrusted-value"
	if _, err := Parse(func(k string) string { return env[k] }); err == nil {
		t.Fatal("invalid proxy accepted")
	}
}

func vectorGroup() map[string]string {
	return map[string]string{"EMBEDDING_BASE_URL": "https://embed.example.internal/v1", "EMBEDDING_MODEL": "test-embed", "EMBEDDING_API_KEY": "test-embed-key", "EMBEDDING_DIMENSION": "1024", "QDRANT_BASE_URL": "http://qdrant:6333"}
}

func TestDialogueDefaultsAndBoundedNumbers(t *testing.T) {
	env := valid()
	c, err := Parse(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.DialogueTimeout != 60 || c.DialogueMaxOutputTokens != 2048 || c.HistoryMaxMessages != 12 || c.HistoryMaxCharacters != 24000 {
		t.Fatalf("unexpected dialogue defaults: %+v", c)
	}
	for _, tc := range []struct{ name, value string }{
		{"DIALOGUE_TIMEOUT_SECONDS", "0"}, {"DIALOGUE_TIMEOUT_SECONDS", "601"},
		{"DIALOGUE_TIMEOUT_SECONDS", "abc"}, {"DIALOGUE_MAX_OUTPUT_TOKENS", "0"},
		{"DIALOGUE_MAX_OUTPUT_TOKENS", "100001"}, {"HISTORY_MAX_MESSAGES", "0"},
		{"HISTORY_MAX_MESSAGES", "101"}, {"HISTORY_MAX_CHARACTERS", "0"},
		{"HISTORY_MAX_CHARACTERS", "1000001"},
	} {
		bad := valid()
		bad[tc.name] = tc.value
		_, err := Parse(func(k string) string { return bad[k] })
		if err == nil || !strings.Contains(err.Error(), tc.name) {
			t.Errorf("accepted invalid %s=%s", tc.name, tc.value)
		}
	}
}

func TestVectorPipelineAllOrNothing(t *testing.T) {
	full := valid()
	for k, v := range vectorGroup() {
		full[k] = v
	}
	c, err := Parse(func(k string) string { return full[k] })
	if err != nil {
		t.Fatal(err)
	}
	if !c.VectorEnabled() || c.EmbeddingDimension != 1024 || c.QdrantCollection != "campusclaw_chunks" || c.QdrantTimeout != 10 || c.EmbeddingTimeout != 30 {
		t.Fatalf("unexpected vector config: %+v", c)
	}
	for name := range vectorGroup() {
		partial := valid()
		for k, v := range vectorGroup() {
			partial[k] = v
		}
		delete(partial, name)
		if _, err := Parse(func(k string) string { return partial[k] }); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("partial vector config accepted (missing %s)", name)
		}
	}
	off := valid()
	c2, err := Parse(func(k string) string { return off[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c2.VectorEnabled() {
		t.Fatal("vector pipeline must be off without embedding config")
	}
	// Compose and .env.example provide a Qdrant URL even for keyword-only
	// deployments. That default must not by itself enable the vector pipeline.
	off["QDRANT_BASE_URL"] = "http://qdrant:6333"
	c3, err := Parse(func(k string) string { return off[k] })
	if err != nil || c3.VectorEnabled() {
		t.Fatalf("qdrant default must not enable vectors: %+v, %v", c3, err)
	}
	off["QDRANT_API_KEY"] = "configured-key"
	if _, err := Parse(func(k string) string { return off[k] }); err == nil || !strings.Contains(err.Error(), "EMBEDDING_BASE_URL") {
		t.Fatal("qdrant key without embedding pipeline accepted")
	}
	badDim := valid()
	for k, v := range vectorGroup() {
		badDim[k] = v
	}
	badDim["EMBEDDING_DIMENSION"] = "8"
	if _, err := Parse(func(k string) string { return badDim[k] }); err == nil {
		t.Fatal("out-of-range dimension accepted")
	}
}

func TestSecretsNeverAppearInErrors(t *testing.T) {
	bad := valid()
	bad["HISTORY_MAX_MESSAGES"] = "0"
	_, err := Parse(func(k string) string { return bad[k] })
	if err == nil {
		t.Fatal("expected error")
	}
	for _, secret := range []string{bad["DIALOGUE_API_KEY"], bad["DIALOGUE_BASE_URL"], bad["SESSION_SECRET"]} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("secret value printed in configuration error")
		}
	}
}
