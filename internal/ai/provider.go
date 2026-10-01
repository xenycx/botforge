// Package ai contains protocol adapters and safety primitives for the AI
// operator. It has no knowledge of users, permissions, or live workspaces.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Usage struct{ PromptTokens, CompletionTokens, TotalTokens int64 }
type Response struct {
	Content      string
	Calls        []ToolCall
	Usage        Usage
	FinishReason string
}

type Client struct{ HTTP *http.Client }
type Config struct {
	BaseURL, ChatPath, ModelsPath, Key, Model string
	Timeout                                   time.Duration
	MaxTokens                                 int64
	Temperature                               float64
}

type ProviderError struct {
	Status        int
	Code, Message string
	RetryAfter    time.Duration
}

func (e *ProviderError) Error() string   { return e.Message }
func (e *ProviderError) Temporary() bool { return e.Status == 429 || e.Status >= 500 || e.Status == 0 }

func endpoint(base, p string) (string, error) {
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		return "", errors.New("invalid provider URL")
	}
	r, e := url.Parse(p)
	if e != nil {
		return "", e
	}
	return u.ResolveReference(r).String(), nil
}

func (c *Client) client(timeout time.Duration) *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = 32
	tr.MaxIdleConnsPerHost = 8
	tr.IdleConnTimeout = 90 * time.Second
	return &http.Client{Transport: tr, Timeout: timeout}
}

func auth(req *http.Request, key string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

func parseProviderError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	msg := strings.TrimSpace(string(b))
	var v struct {
		Error struct {
			Message string          `json:"message"`
			Code    json.RawMessage `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &v) == nil && v.Error.Message != "" {
		msg = v.Error.Message
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	// The HTTP status decides the category; provider codes differ between
	// vendors and may be strings, numbers or null.
	code := "provider"
	switch resp.StatusCode {
	case 401, 403:
		code = "authentication"
	case 402:
		code = "balance"
	case 429:
		code = "rate_limit"
		if strings.Contains(string(v.Error.Code), "insufficient_quota") {
			code = "balance"
		}
	}
	var retry time.Duration
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, e := strconv.Atoi(s); e == nil {
			retry = time.Duration(n) * time.Second
		}
	}
	return &ProviderError{Status: resp.StatusCode, Code: code, Message: msg, RetryAfter: retry}
}

// Models discovers model IDs. Providers that do not support the endpoint
// return a ProviderError so the administration UI can offer manual entry.
func (c *Client) Models(ctx context.Context, cfg Config) ([]string, error) {
	u, e := endpoint(cfg.BaseURL, cfg.ModelsPath)
	if e != nil {
		return nil, e
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	auth(req, cfg.Key)
	resp, e := c.client(cfg.Timeout).Do(req)
	if e != nil {
		return nil, &ProviderError{Code: "network", Message: e.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, parseProviderError(resp)
	}
	var v struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&v); e != nil {
		return nil, e
	}
	out := make([]string, 0, len(v.Data))
	for _, m := range v.Data {
		if m.ID != "" && len(m.ID) <= 160 {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	} `json:"usage"`
}

// Complete streams a chat completion, assembling fragmented parallel tool
// calls by their protocol index. onDelta receives displayable assistant text.
func (c *Client) Complete(ctx context.Context, cfg Config, msgs []Message, tools []Tool, onDelta func(string)) (Response, error) {
	u, e := endpoint(cfg.BaseURL, cfg.ChatPath)
	if e != nil {
		return Response{}, e
	}
	body := map[string]any{"model": cfg.Model, "messages": msgs, "stream": true, "stream_options": map[string]any{"include_usage": true}, "max_tokens": cfg.MaxTokens, "temperature": cfg.Temperature}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	auth(req, cfg.Key)
	resp, e := c.client(cfg.Timeout).Do(req)
	if e != nil {
		return Response{}, &ProviderError{Code: "network", Message: e.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return Response{}, parseProviderError(resp)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "text/event-stream") {
		return Response{}, &ProviderError{Code: "malformed_stream", Message: "provider did not return an SSE stream"}
	}
	type partial struct{ id, typ, name, args string }
	calls := map[int]*partial{}
	var out Response
	scan := bufio.NewScanner(io.LimitReader(resp.Body, 16<<20))
	scan.Buffer(make([]byte, 64<<10), 2<<20)
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ch streamChunk
		if e := json.Unmarshal([]byte(data), &ch); e != nil {
			return out, &ProviderError{Code: "malformed_stream", Message: "provider returned malformed streaming JSON"}
		}
		if ch.Usage != nil {
			out.Usage = Usage{ch.Usage.PromptTokens, ch.Usage.CompletionTokens, ch.Usage.TotalTokens}
		}
		for _, choice := range ch.Choices {
			d := choice.Delta
			if d.Content != "" {
				out.Content += d.Content
				if onDelta != nil {
					onDelta(d.Content)
				}
			}
			if choice.FinishReason != nil {
				out.FinishReason = *choice.FinishReason
			}
			for _, tc := range d.ToolCalls {
				p := calls[tc.Index]
				if p == nil {
					p = &partial{}
					calls[tc.Index] = p
				}
				p.id += tc.ID
				p.typ += tc.Type
				p.name += tc.Function.Name
				p.args += tc.Function.Arguments
			}
		}
	}
	if e := scan.Err(); e != nil {
		return out, &ProviderError{Code: "malformed_stream", Message: e.Error()}
	}
	for i := 0; i < len(calls); i++ {
		p := calls[i]
		if p == nil {
			continue
		}
		if p.typ == "" {
			p.typ = "function"
		}
		out.Calls = append(out.Calls, ToolCall{ID: p.id, Type: p.typ, Function: ToolCallFunction{Name: p.name, Arguments: p.args}})
	}
	return out, nil
}

// Test performs a no-project-data capability check: streaming, usage, and a
// harmless tool call. Authentication failures remain distinguishable.
func (c *Client) Test(ctx context.Context, cfg Config) (map[string]bool, error) {
	tools := []Tool{{Type: "function", Function: ToolFunction{Name: "echo_capability", Description: "Return the provided value", Parameters: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"}}}}}
	r, e := c.Complete(ctx, cfg, []Message{{Role: "user", Content: "Call echo_capability with value ok. Do not answer with prose."}}, tools, nil)
	if e != nil {
		return nil, e
	}
	return map[string]bool{"authentication": true, "streaming": true, "usage": r.Usage.TotalTokens > 0, "tool_call": len(r.Calls) > 0}, nil
}

func FriendlyError(err error) (code, message string) {
	var p *ProviderError
	if errors.As(err, &p) {
		switch p.Code {
		case "authentication":
			return p.Code, "The provider rejected the API key. Update the encrypted key and test again."
		case "balance":
			return p.Code, "The provider reports insufficient balance or quota."
		case "rate_limit":
			return p.Code, "The provider is rate limiting requests. Retry after the shown delay."
		case "malformed_stream":
			return p.Code, "The provider returned an invalid streaming response."
		}
		return p.Code, "The AI provider could not complete the request."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", "The AI provider timed out."
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled", "The run was cancelled."
	}
	return "provider", "The AI provider could not complete the request."
}
