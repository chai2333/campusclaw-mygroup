// OpenAI-compatible streaming chat-completions client implementing the
// Gateway interface. Provider details stay server-side; errors map to
// ErrUnavailable / ErrTimeout only, never leaking credentials or payloads.
package dialogue

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPGateway streams chat completions from an OpenAI-compatible endpoint.
type HTTPGateway struct {
	BaseURL string // e.g. https://api.example.com/v1
	Model   string
	APIKey  string
	Timeout time.Duration
	HTTP    *http.Client // optional override; zero value uses a default
}

type chatRequest struct {
	Model      string        `json:"model"`
	Stream     bool          `json:"stream"`
	MaxTokens  int           `json:"max_tokens,omitempty"`
	Messages   []chatMessage `json:"messages"`
	Tools      []Tool        `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
}

type chatMessage = Message

type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Stream calls the provider with stream=true and forwards each non-empty
// delta. It returns the accumulated full text. ctx cancellation, response
// body read failure and non-200 map to ErrUnavailable; deadline/cancellation
// of the caller's own deadline maps to ErrTimeout.
func (g *HTTPGateway) Stream(ctx context.Context, req Request, onDelta func(text string) error) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:     firstNonEmpty(req.Model, g.Model),
		Stream:    true,
		MaxTokens: req.MaxTokens,
		Messages:  toChatMessages(req.Messages),
	})
	if err != nil {
		return "", ErrUnavailable
	}
	httpClient := g.HTTP
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	if g.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.Timeout)
		defer cancel()
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(g.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", ErrUnavailable
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if g.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+g.APIKey)
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", ErrTimeout
		}
		return "", ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", ErrUnavailable
	}

	var accumulated strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return accumulated.String(), err
		}
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if bytes.Equal(payload, []byte("[DONE]")) {
			break
		}
		var chunk chatStreamChunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return accumulated.String(), ErrUnavailable
		}
		if chunk.Error != nil {
			return accumulated.String(), ErrUnavailable
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			accumulated.WriteString(choice.Delta.Content)
			if onDelta != nil {
				if err := onDelta(choice.Delta.Content); err != nil {
					return accumulated.String(), err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return accumulated.String(), ErrTimeout
		}
		return accumulated.String(), ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return accumulated.String(), err
	}
	return accumulated.String(), nil
}

func toChatMessages(messages []Message) []chatMessage {
	out := make([]chatMessage, 0, len(messages))
	for _, m := range messages {
		out = append(out, m)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// NewHTTPGateway builds the configured provider gateway from settings.
func NewHTTPGateway(baseURL, model, apiKey string, timeout time.Duration) (*HTTPGateway, error) {
	if baseURL == "" || model == "" {
		return nil, fmt.Errorf("dialogue gateway configuration incomplete")
	}
	return &HTTPGateway{BaseURL: baseURL, Model: model, APIKey: apiKey, Timeout: timeout}, nil
}
