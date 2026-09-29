// Package config loads server-only configuration without logging secret values.
package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Environment, SessionSecret, DBHost, DBName, DBUser, DBPassword, UploadDir   string
	TeacherPassword, StudentPassword                                            string
	DBPort, MaxUploadBytes, SessionTTL, MaxFailures, WindowSeconds, LockSeconds int
	TrustedProxies                                                              []netip.Prefix
}

func Load() (Config, error) { return Parse(os.Getenv) }

func Parse(get func(string) string) (Config, error) {
	var c Config
	required := []struct {
		name   string
		target *string
	}{
		{"SESSION_SECRET", &c.SessionSecret}, {"DB_HOST", &c.DBHost}, {"DB_NAME", &c.DBName},
		{"DB_USER", &c.DBUser}, {"DB_PASSWORD", &c.DBPassword}, {"UPLOAD_DIR", &c.UploadDir},
		{"SEED_TEACHER_PASSWORD", &c.TeacherPassword}, {"SEED_STUDENT_PASSWORD", &c.StudentPassword},
	}
	for _, field := range required {
		*field.target = get(field.name)
		if strings.TrimSpace(*field.target) == "" {
			return c, fmt.Errorf("%s is required", field.name)
		}
	}
	for _, field := range []struct{ name, value string }{{"SEED_TEACHER_PASSWORD", c.TeacherPassword}, {"SEED_STUDENT_PASSWORD", c.StudentPassword}} {
		if len([]byte(field.value)) > 72 {
			return c, fmt.Errorf("%s must not exceed 72 bytes", field.name)
		}
	}
	c.Environment = get("APP_ENV")
	if c.Environment == "" {
		c.Environment = "development"
	}
	if c.Environment != "development" && c.Environment != "production" && c.Environment != "test" {
		return c, fmt.Errorf("APP_ENV must be development, production or test")
	}
	numbers := []struct {
		name     string
		fallback int
		target   *int
	}{
		{"DB_PORT", 3306, &c.DBPort}, {"MAX_UPLOAD_BYTES", 2097152, &c.MaxUploadBytes},
		{"SESSION_TTL_SECONDS", 28800, &c.SessionTTL}, {"LOGIN_MAX_FAILURES", 5, &c.MaxFailures},
		{"LOGIN_WINDOW_SECONDS", 300, &c.WindowSeconds}, {"LOGIN_LOCK_SECONDS", 300, &c.LockSeconds},
	}
	for _, field := range numbers {
		raw := get(field.name)
		if raw == "" {
			*field.target = field.fallback
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return c, fmt.Errorf("%s must be a positive integer", field.name)
		}
		*field.target = value
	}
	if c.DBPort > 65535 {
		return c, fmt.Errorf("DB_PORT must be at most 65535")
	}
	for _, raw := range strings.Split(get("TRUSTED_PROXY_CIDRS"), ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return c, fmt.Errorf("TRUSTED_PROXY_CIDRS must contain valid CIDRs")
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix)
	}
	return c, nil
}
