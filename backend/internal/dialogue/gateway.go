// Package dialogue defines the server-side dialogue gateway abstraction used
// by guided conversations and cited answers. Implementations talk to an
// OpenAI-compatible chat completions endpoint; tests use deterministic fakes
// so no external network access is required.
package dialogue

import (
	"context"
	"errors"
)

// Message is one chat message in a completion request.
type Message struct {
	Role    string // "system", "user" or "assistant"
	Content string
}

// Request is one completion call. Implementations must not mutate it.
type Request struct {
	Model     string
	Messages  []Message
	MaxTokens int
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
