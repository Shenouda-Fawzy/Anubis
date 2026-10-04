package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// OpenAIClient calls /chat/completions on OpenAI and OpenAI-compatible servers.
type OpenAIClient struct {
	BaseURL      string
	APIKey       string
	Organization string
	Model        string
	HTTPClient   *http.Client
}

func (o *OpenAIClient) ModelName() string {
	if o == nil {
		return ""
	}
	return o.Model
}

func NewOpenAIClient(apiKey, baseURL, model string) *OpenAIClient {
	return &OpenAIClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: &http.Client{Timeout: httpTimeout},
	}
}

type completionResponse struct {
	Choices []*Choice    `json:"choices"`
	Usage   *Usage       `json:"usage"`
	Err     *errResponse `json:"error"`
}

func (c *completionResponse) TextContent() string {
	if c == nil {
		return ""
	}
	if len(c.Choices) == 0 {
		return ""
	}
	if c.Choices[0].FinishReason != "stop" {
		return ""
	}
	if c.Choices[0].Message == nil {
		return ""
	}
	return c.Choices[0].Message.Content
}

// Returns true if the message is completed (finish reason = stop)
func (c *completionResponse) Finished() bool {
	if c == nil {
		return false
	}
	if len(c.Choices) == 0 {
		return false
	}
	return c.Choices[0].FinishReason == "stop"
}

func (c *completionResponse) FinishReason() string {
	if c == nil {
		return ""
	}
	if len(c.Choices) == 0 {
		return "n/a"
	}
	return c.Choices[0].FinishReason
}

func (c *completionResponse) HasError() bool {
	if c == nil {
		return true
	}
	return c.Err != nil
}

type errResponse struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    int    `json:"code"`
}

type Choice struct {
	Message      *Message `json:"message"`
	FinishReason string   `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (o *OpenAIClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if o == nil {
		return nil, errors.New("OpenAIClient invalid")
	}
	if o.BaseURL == "" {
		return nil, errors.New("no model base URL configured: set -llm-base-url or ANUBIS_LLM_BASE_URL")
	}
	if o.APIKey == "" {
		return nil, errors.New("no model API key configured: set ANUBIS_LLM_API_KEY to your provider's key (see docs/providers.md)")
	}
	url := strings.TrimRight(o.BaseURL, "/")
	if strings.HasSuffix(url, "/chat/completions") == false {
		url += "/chat/completions"
	}
	body, err := json.Marshal(req)
	if err != nil {
		slog.Error("failed to marshal completion request", "error", err)
		return nil, err
	}
	slog.Debug("api request", "url", url, "model", req.Model, "bytes", len(body))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	resp, err := o.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	slog.Debug("api response status", "status", resp.Status)
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 30<<20))
	if err != nil {
		slog.Error("failed to read response body", "error", err)
		return nil, err
	}
	slog.Debug("api response", "bytes", len(data))

	// A rate limit or an outage is the most likely production failure, so the
	// status line must reach the operator. Without this the provider's error
	// body is parsed as if it were a completion and reported as an unhelpful
	// parse failure with no indication of why.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llm: provider returned %s: %s", resp.Status, snippet(data))
	}

	d, err := ParseResponse(data)
	if err != nil {
		slog.Error("failed to parse response", "error", err)
		return nil, err
	}
	if d.HasError() {
		slog.Error("completion failed", "message", d.Err.Message, "code", d.Err.Code, "type", d.Err.Type, "param", d.Err.Param)
		return nil, fmt.Errorf("model provider returned an error: %s (type=%s, code=%d, param=%s)", d.Err.Message, d.Err.Type, d.Err.Code, d.Err.Param)
	}
	// Content that stopped early is not a review. Returning it would let a
	// truncated answer read as a clean bill of health, so the reason is named
	// and the agent is recorded as failed.
	if d.Finished() == false {
		return nil, fmt.Errorf("model response incomplete: finish_reason=%q (content discarded)", d.FinishReason())
	}
	cr := CompletionResponse{
		Content:      d.TextContent(),
		FinishReason: d.FinishReason(),
		PromptTokens: d.Usage.promptTokens(),
		OutputTokens: d.Usage.completionTokens(),
		TotalTokens:  d.Usage.totalTokens(),
	}
	slog.Debug("completion finished", "model", o.Model, "prompt_tokens", cr.PromptTokens, "completion_tokens", cr.OutputTokens, "total_tokens", cr.TotalTokens, "finish_reason", cr.FinishReason)
	return &cr, nil
}

func ParseResponse(data []byte) (*completionResponse, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	switch trimmed[0] {
	case '[':
		var arr []completionResponse
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, err
		}
		if len(arr) == 0 {
			return nil, fmt.Errorf("empty array response")
		}
		return &arr[0], nil
	case '{':
		var resp completionResponse
		if err := json.Unmarshal(trimmed, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	default:
		return nil, fmt.Errorf("unexpected response format")
	}
}

// snippet reduces a provider error body to a short single-line string safe to
// put in a log and in an error message. Providers return everything from a
// one-line JSON error to a full HTML error page, and neither control
// characters nor a multi-kilobyte stack trace belongs in the review log.
func snippet(data []byte) string {
	const maxSnippetLen = 200
	s := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, string(data))
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "(empty body)"
	}
	if len(s) > maxSnippetLen {
		return s[:maxSnippetLen] + "..."
	}
	return s
}

func (u *Usage) promptTokens() int {
	if u == nil {
		return 0
	}
	return u.PromptTokens
}

func (u *Usage) completionTokens() int {
	if u == nil {
		return 0
	}
	return u.CompletionTokens
}

func (u *Usage) totalTokens() int {
	if u == nil {
		return 0
	}
	return u.TotalTokens
}
