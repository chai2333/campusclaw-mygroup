package dialogue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Decide is a bounded native function-call round. The final user-facing prose
// still uses Stream; tool calls cannot be smuggled through prose parsing.
func (g *HTTPGateway) Decide(ctx context.Context, req Request) (Decision, error) {
	body, err := json.Marshal(chatRequest{Model: firstNonEmpty(req.Model, g.Model), Stream: false, MaxTokens: req.MaxTokens, Messages: toChatMessages(req.Messages), Tools: req.Tools})
	if err != nil {
		return Decision{}, ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(g.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Decision{}, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+g.APIKey)
	client := g.HTTP
	if client == nil {
		client = &http.Client{Timeout: g.Timeout}
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Decision{}, ErrTimeout
		}
		return Decision{}, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Decision{}, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 131073))
	if err != nil || len(raw) > 131072 {
		return Decision{}, ErrUnavailable
	}
	var parsed struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &parsed) != nil || len(parsed.Choices) != 1 {
		return Decision{}, ErrUnavailable
	}
	m := parsed.Choices[0].Message
	if len(m.ToolCalls) > 16 {
		return Decision{}, ErrUnavailable
	}
	seen := map[string]bool{}
	for _, call := range m.ToolCalls {
		if call.ID == "" || len(call.ID) > 128 || seen[call.ID] || call.Type != "function" || len(call.Function.Arguments) > 8192 {
			return Decision{}, ErrUnavailable
		}
		seen[call.ID] = true
	}
	return Decision{Content: m.Content, Calls: m.ToolCalls}, nil
}
