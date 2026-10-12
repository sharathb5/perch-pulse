package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxResponseBytes    = 256 * 1024
	chatCompletionsPath = "/chat/completions"
)

// ChatClient calls an OpenAI-compatible Chat Completions API.
// Contract (POST {base}/chat/completions):
//
// Request JSON fields used:
//   - model (string, required)
//   - messages ([]{role, content}, required) — roles: system | user (developer mapped to system for broad compatibility)
//   - max_tokens (int)
//   - temperature (number)
//   - response_format ({"type":"json_object"}) when supported
//
// Response JSON fields used:
//   - id, model
//   - choices[0].message.content, choices[0].finish_reason
//   - usage.prompt_tokens, usage.completion_tokens
//   - error.message (on failure bodies)
//
// No official SDK dependency. Live provider behavior may differ; CI uses FakeClient only.
type ChatClient interface {
	Complete(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// ChatRequest is the bounded completion request.
type ChatRequest struct {
	Model       string
	System      string
	User        string
	MaxTokens   int
	Temperature float64
}

// ChatResponse is the parsed completion content (no secrets).
type ChatResponse struct {
	Content          string
	FinishReason     string
	Model            string
	RequestID        string
	PromptTokens     int
	CompletionTokens int
}

// HTTPChatClient implements ChatClient against an OpenAI-style endpoint.
type HTTPChatClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type chatAPIRequest struct {
	Model          string              `json:"model"`
	Messages       []chatAPIMessage    `json:"messages"`
	MaxTokens      int                 `json:"max_tokens"`
	Temperature    float64             `json:"temperature"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
}

type chatAPIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponseFormat struct {
	Type string `json:"type"`
}

type chatAPIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Complete performs one bounded chat completion call.
func (c *HTTPChatClient) Complete(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if c == nil {
		return ChatResponse{}, fmt.Errorf("llm: chat client not configured")
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return ChatResponse{}, &UnavailableError{Reason: "API key missing"}
	}
	base, err := ValidateProviderBaseURL(c.BaseURL)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: invalid base URL: %w", err)
	}
	endpoint, err := joinChatURL(base)
	if err != nil {
		return ChatResponse{}, err
	}
	body := chatAPIRequest{
		Model: req.Model,
		Messages: []chatAPIMessage{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.User},
		},
		MaxTokens:      req.MaxTokens,
		Temperature:    req.Temperature,
		ResponseFormat: &chatResponseFormat{Type: "json_object"},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 20 * time.Second,
			// Never follow redirects with Authorization — avoids leaking the API key
			// onto an unvalidated (possibly http) Location.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	} else if httpClient.CheckRedirect == nil {
		// Clone shallowly so callers' clients are not mutated unexpectedly.
		cloned := *httpClient
		cloned.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
		httpClient = &cloned
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: provider request failed")
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("llm: provider response unreadable")
	}
	if len(respBody) > maxResponseBytes {
		return ChatResponse{}, &ValidationError{Msg: "provider response exceeds size bound"}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return ChatResponse{}, &RateLimitError{Msg: "provider rate limited"}
	}
	// Redirects are refused (see CheckRedirect); treat them as provider misconfiguration.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return ChatResponse{}, fmt.Errorf("llm: provider redirected (refused to follow with credentials)")
	}
	var parsed chatAPIResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return ChatResponse{}, fmt.Errorf("llm: provider HTTP %d", resp.StatusCode)
		}
		return ChatResponse{}, &ValidationError{Msg: "provider returned malformed JSON"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Never echo provider error bodies — they may contain echoed credentials.
		return ChatResponse{}, fmt.Errorf("llm: provider HTTP %d", resp.StatusCode)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return ChatResponse{}, fmt.Errorf("llm: provider rejected request")
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return ChatResponse{}, &ValidationError{Msg: "provider returned empty completion"}
	}
	content := parsed.Choices[0].Message.Content
	if utf8.RuneCountInString(content) > 100_000 {
		return ChatResponse{}, &ValidationError{Msg: "provider content too large"}
	}
	out := ChatResponse{
		Content:          content,
		FinishReason:     parsed.Choices[0].FinishReason,
		Model:            firstNonEmpty(parsed.Model, req.Model),
		RequestID:        parsed.ID,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}
	return out, nil
}

func joinChatURL(base *url.URL) (string, error) {
	u := *base
	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, chatCompletionsPath) {
		u.Path = path
	} else {
		u.Path = path + chatCompletionsPath
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// FakeClient is a deterministic test double — never calls the network.
type FakeClient struct {
	Response ChatResponse
	Err      error
	Calls    int
	LastReq  ChatRequest
	Handler  func(ChatRequest) (ChatResponse, error)
}

// Complete records the call and returns the configured response.
func (f *FakeClient) Complete(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	_ = ctx
	f.Calls++
	f.LastReq = req
	if f.Handler != nil {
		return f.Handler(req)
	}
	if f.Err != nil {
		return ChatResponse{}, f.Err
	}
	return f.Response, nil
}
