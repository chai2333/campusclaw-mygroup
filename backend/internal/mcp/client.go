// Package mcp owns the authenticated, bounded HTTP tools client and class registry.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

var ErrRemote = errors.New("MCP连接或协议响应无效，请检查地址、令牌与服务状态")
var ErrNotFound = errors.New("not_found")
var ErrConflict = errors.New("配置已改变，请重新加载后操作")
var ToolName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
var ServerName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

func ParseRef(ref string) (string, string, bool) {
	tool, server, found := strings.Cut(ref, "@")
	return tool, server, found && ToolName.MatchString(tool) && ServerName.MatchString(server)
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations struct {
		ReadOnly    bool `json:"readOnlyHint"`
		Destructive bool `json:"destructiveHint"`
		OpenWorld   bool `json:"openWorldHint"`
	} `json:"annotations"`
	Approved            bool   `json:"approved"`
	PreviousDescription string `json:"previous_description,omitempty"`
}

func (t Tool) Safe() bool {
	return t.Annotations.ReadOnly && !t.Annotations.Destructive && !t.Annotations.OpenWorld
}
func compileSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	if len(raw) > 16384 {
		return nil, ErrRemote
	}
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil || root["type"] != "object" {
		return nil, ErrRemote
	}
	c := jsonschema.NewCompiler()
	c.LoadURL = func(string) (io.ReadCloser, error) { return nil, errors.New("external schema loading disabled") }
	if err := c.AddResource("https://schema.invalid/input.json", bytes.NewReader(raw)); err != nil {
		return nil, ErrRemote
	}
	return c.Compile("https://schema.invalid/input.json")
}
func ValidateArguments(raw json.RawMessage, args string) error {
	schema, err := compileSchema(raw)
	if err != nil {
		return ErrRemote
	}
	if len(args) > 8192 {
		return errors.New("参数过长")
	}
	var v any
	if json.Unmarshal([]byte(args), &v) != nil {
		return errors.New("参数须为JSON对象")
	}
	if _, ok := v.(map[string]any); !ok {
		return errors.New("参数须为JSON对象")
	}
	if schema.Validate(v) != nil {
		return errors.New("参数与工具输入结构不匹配")
	}
	return nil
}

type Client struct{ HTTP *http.Client }

func (c Client) rpc(ctx context.Context, base, token, method string, params any, target any) error {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/mcp", bytes.NewReader(body))
	if err != nil {
		return ErrRemote
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(req)
	if err != nil {
		return ErrRemote
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return ErrRemote
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 262145))
	if err != nil || len(raw) > 262144 {
		return ErrRemote
	}
	var envelope struct {
		Version string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Version != "2.0" || string(envelope.ID) != "1" || len(envelope.Error) > 0 || len(envelope.Result) == 0 {
		return ErrRemote
	}
	if json.Unmarshal(envelope.Result, target) != nil {
		return ErrRemote
	}
	return nil
}
func (c Client) Discover(ctx context.Context, base, token string) ([]Tool, error) {
	var init struct {
		Version      string                     `json:"protocolVersion"`
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := c.rpc(ctx, base, token, "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "campusclaw", "version": "1.0"}}, &init); err != nil {
		return nil, err
	}
	if init.Version != "2025-03-26" || init.Capabilities["tools"] == nil {
		return nil, ErrRemote
	}
	// Stateless HTTP server accepts the initialized notification without session state.
	notification, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	req, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/mcp", bytes.NewReader(notification))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, ErrRemote
	}
	resp.Body.Close()
	if resp.StatusCode != 202 && resp.StatusCode != 204 {
		return nil, ErrRemote
	}
	var result struct {
		Tools      []Tool `json:"tools"`
		NextCursor string `json:"nextCursor"`
	}
	if err := c.rpc(ctx, base, token, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	if len(result.Tools) == 0 || len(result.Tools) > 64 || result.NextCursor != "" {
		return nil, ErrRemote
	}
	seen := map[string]bool{}
	for _, t := range result.Tools {
		if !ToolName.MatchString(t.Name) || seen[t.Name] || strings.TrimSpace(t.Description) == "" || len(t.Description) > 8000 {
			return nil, ErrRemote
		}
		seen[t.Name] = true
		if _, err := compileSchema(t.InputSchema); err != nil {
			return nil, ErrRemote
		}
	}
	return result.Tools, nil
}
func (c Client) Call(ctx context.Context, base, token, name, args string) (string, error) {
	var arguments any
	if json.Unmarshal([]byte(args), &arguments) != nil {
		return "", ErrRemote
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.rpc(ctx, base, token, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return "", err
	}
	if result.IsError {
		return "", errors.New("远端工具执行失败，请检查参数或稍后重试")
	}
	texts := []string{}
	for _, item := range result.Content {
		if item.Type == "text" {
			texts = append(texts, item.Text)
		}
	}
	text := strings.Join(texts, "\n")
	if strings.TrimSpace(text) == "" {
		return "", errors.New("工具未返回文本")
	}
	return text, nil
}
