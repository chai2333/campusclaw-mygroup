package config

import (
	"strings"
	"testing"
)

func valid() map[string]string {
	return map[string]string{"SESSION_SECRET": "test-secret-do-not-deploy", "DB_HOST": "db", "DB_NAME": "campusclaw", "DB_USER": "campusclaw", "DB_PASSWORD": "test-database-password", "UPLOAD_DIR": "/data/uploads", "SEED_TEACHER_PASSWORD": "teacher-fixture", "SEED_STUDENT_PASSWORD": "student-fixture"}
}
func TestRequiredSecretsAndFields(t *testing.T) {
	for name := range valid() {
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
