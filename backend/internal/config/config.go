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
	MCPAuthToken, MCPDefaultURL, MCPAllowedURLs                                 string
	MCPMaxRounds, MCPMaxTurnCalls, MCPMaxConversationCalls                      int
	Environment, SessionSecret, DBHost, DBName, DBUser, DBPassword, UploadDir   string
	TeacherPassword, StudentPassword                                            string
	DBPort, MaxUploadBytes, SessionTTL, MaxFailures, WindowSeconds, LockSeconds int
	TrustedProxies                                                              []netip.Prefix

	DialogueBaseURL, DialogueModel, DialogueAPIKey    string
	DialogueTimeout, DialogueMaxOutputTokens          int
	HistoryMaxMessages, HistoryMaxCharacters          int
	EmbeddingBaseURL, EmbeddingModel, EmbeddingAPIKey string
	EmbeddingDimension, EmbeddingTimeout              int
	QdrantBaseURL, QdrantAPIKey, QdrantCollection     string
	QdrantTimeout                                     int
}

// VectorEnabled reports whether the optional embedding and vector pipeline is
// fully configured. It is all-or-nothing: a partial configuration is rejected
// at startup instead of being partially enabled.
func (c Config) VectorEnabled() bool { return c.EmbeddingBaseURL != "" }

// DialogueEnabled reports whether a real OpenAI-compatible dialogue gateway
// has been configured. Dialogue is optional so the knowledge base can still
// serve retrieval while an operator is obtaining model credentials.
func (c Config) DialogueEnabled() bool { return c.DialogueBaseURL != "" }

func Load() (Config, error) { return Parse(os.Getenv) }

func Parse(get func(string) string) (Config, error) {
	var c Config
	c.MCPAuthToken = get("MCP_AUTH_TOKEN")
	c.MCPDefaultURL = get("MCP_DEFAULT_BASE_URL")
	if c.MCPDefaultURL == "" {
		c.MCPDefaultURL = "http://mcp-demo:8090"
	}
	c.MCPAllowedURLs = get("MCP_ALLOWED_BASE_URLS")
	if c.MCPAllowedURLs == "" {
		c.MCPAllowedURLs = c.MCPDefaultURL
	}
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
	// Dialogue is optional, but partial configuration is not. This prevents a
	// stale or placeholder endpoint from being treated as a usable model.
	dialogueGroup := []struct {
		name   string
		target *string
	}{
		{"DIALOGUE_BASE_URL", &c.DialogueBaseURL},
		{"DIALOGUE_MODEL", &c.DialogueModel},
		{"DIALOGUE_API_KEY", &c.DialogueAPIKey},
	}
	dialogueAnySet := false
	for _, field := range dialogueGroup {
		*field.target = strings.TrimSpace(get(field.name))
		if *field.target != "" {
			dialogueAnySet = true
		}
	}
	if dialogueAnySet {
		for _, field := range dialogueGroup {
			if *field.target == "" {
				return c, fmt.Errorf("%s is required when the dialogue gateway is configured", field.name)
			}
		}
	}
	bounded := []struct {
		name     string
		fallback int
		target   *int
		min, max int
	}{
		{"MCP_MAX_ROUNDS", 3, &c.MCPMaxRounds, 1, 10},
		{"MCP_MAX_TURN_CALLS", 6, &c.MCPMaxTurnCalls, 1, 32},
		{"MCP_MAX_CONVERSATION_CALLS", 12, &c.MCPMaxConversationCalls, 1, 200},
		{"DIALOGUE_TIMEOUT_SECONDS", 60, &c.DialogueTimeout, 1, 600},
		{"DIALOGUE_MAX_OUTPUT_TOKENS", 2048, &c.DialogueMaxOutputTokens, 1, 100000},
		{"HISTORY_MAX_MESSAGES", 12, &c.HistoryMaxMessages, 1, 100},
		{"HISTORY_MAX_CHARACTERS", 24000, &c.HistoryMaxCharacters, 1, 1000000},
	}
	for _, field := range bounded {
		raw := get(field.name)
		if raw == "" {
			*field.target = field.fallback
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return c, fmt.Errorf("%s must be an integer", field.name)
		}
		if value < field.min || value > field.max {
			return c, fmt.Errorf("%s must be between %d and %d", field.name, field.min, field.max)
		}
		*field.target = value
	}
	// The embedding and vector pipeline is optional but all-or-nothing:
	// partial configuration must fail startup rather than half-enable it.
	embeddingGroup := []struct {
		name   string
		target *string
	}{
		{"EMBEDDING_BASE_URL", &c.EmbeddingBaseURL},
		{"EMBEDDING_MODEL", &c.EmbeddingModel},
		{"EMBEDDING_API_KEY", &c.EmbeddingAPIKey},
	}
	var dimension string
	anySet := false
	for _, field := range embeddingGroup {
		*field.target = strings.TrimSpace(get(field.name))
		if *field.target != "" {
			anySet = true
		}
	}
	dimension = strings.TrimSpace(get("EMBEDDING_DIMENSION"))
	if dimension != "" {
		anySet = true
	}
	if anySet {
		for _, field := range embeddingGroup {
			if *field.target == "" {
				return c, fmt.Errorf("%s is required when the embedding pipeline is configured", field.name)
			}
		}
		if dimension == "" {
			return c, fmt.Errorf("EMBEDDING_DIMENSION is required when the embedding pipeline is configured")
		}
		value, err := strconv.Atoi(dimension)
		if err != nil || value < 16 || value > 4096 {
			return c, fmt.Errorf("EMBEDDING_DIMENSION must be an integer between 16 and 4096")
		}
		c.EmbeddingDimension = value
		c.QdrantBaseURL = strings.TrimSpace(get("QDRANT_BASE_URL"))
		if c.QdrantBaseURL == "" {
			return c, fmt.Errorf("QDRANT_BASE_URL is required when the embedding pipeline is configured")
		}
		c.QdrantAPIKey = get("QDRANT_API_KEY")
	} else if strings.TrimSpace(get("QDRANT_API_KEY")) != "" {
		return c, fmt.Errorf("EMBEDDING_BASE_URL is required when QDRANT_API_KEY is configured")
	}
	if c.VectorEnabled() {
		boundedOptional := []struct {
			name     string
			fallback int
			target   *int
			min, max int
		}{
			{"EMBEDDING_TIMEOUT_SECONDS", 30, &c.EmbeddingTimeout, 1, 120},
			{"QDRANT_TIMEOUT_SECONDS", 10, &c.QdrantTimeout, 1, 60},
		}
		for _, field := range boundedOptional {
			raw := get(field.name)
			if raw == "" {
				*field.target = field.fallback
				continue
			}
			value, err := strconv.Atoi(raw)
			if err != nil || value < field.min || value > field.max {
				return c, fmt.Errorf("%s must be an integer between %d and %d", field.name, field.min, field.max)
			}
			*field.target = value
		}
		c.QdrantCollection = strings.TrimSpace(get("QDRANT_COLLECTION"))
		if c.QdrantCollection == "" {
			c.QdrantCollection = "campusclaw_chunks"
		}
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
