// Package dialogue defines the server-side dialogue gateway abstraction used
// by guided conversations and cited answers. Implementations talk to an
// OpenAI-compatible chat completions endpoint; tests use deterministic fakes
// so no external network access is required.
package dialogue

import (
	"context"
	"encoding/json"
	"errors"
)

// Message is one chat message in a completion request.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
}
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}
type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function Function `json:"function"`
}
type Decision struct {
	Content string
	Calls   []ToolCall
}
type Planner interface {
	Decide(context.Context, Request) (Decision, error)
}

// Request is one completion call. Implementations must not mutate it.
type Request struct {
	Model     string
	Messages  []Message
	MaxTokens int
	Tools     []Tool
}

// Errors reported by gateway implementations. The orchestrator maps them to
// stable terminal categories (model_unavailable, model_timeout) without
// leaking provider details.
var (
	ErrUnavailable = errors.New("dialogue gateway unavailable")
	ErrTimeout     = errors.New("dialogue gateway timeout")
)

// Gateway produces assistant responses. Stream invokes onDelta once per
// non-empty text fragment in provider order and returns the accumulated full
// text when the provider finishes. If onDelta returns an error, Stream stops
// and returns it unchanged. Implementations must honour ctx cancellation.
type Gateway interface {
	Stream(ctx context.Context, req Request, onDelta func(text string) error) (string, error)
}

// UnavailableGateway keeps HTTP handlers safe when no real model is
// configured. It never fabricates a response.
type UnavailableGateway struct{}

func (UnavailableGateway) Stream(context.Context, Request, func(string) error) (string, error) {
	return "", ErrUnavailable
}
