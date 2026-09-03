package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// OpenCode Zen defaults served by NewOpenAIClient.
const (
	DefaultBaseURL = "https://opencode.ai/zen/v1"
	DefaultModel   = "big-pickle"
)

// OpenAIClient calls /chat/completions on OpenAI and OpenAI-compatible servers.
type OpenAIClient struct {
	BaseURL      string
	APIKey       string
	Organization string
	Model        string
	HTTPClient   *http.Client
}

func NewOpenAIClient(apiKey, baseURL, model string) *OpenAIClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultModel
	}
	return &OpenAIClient{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, HTTPClient: http.DefaultClient}
}

type completionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *OpenAIClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	log.Println("LLM Complete started")
	defer log.Println("LLM Complete Done")
	if c == nil {
		return CompletionResponse{}, fmt.Errorf("llm: nil client")
	}
	if req.Model == "" {
		req.Model = c.Model
	}
	if req.Model == "" {
		return CompletionResponse{}, fmt.Errorf("llm: model is required")
	}
	// Force structured JSON output so the returned completion can be unmarshaled
	// consistently. Callers may override with an explicit ResponseFormat.
	if req.ResponseFormat == nil {
		req.ResponseFormat = &ResponseFormat{Type: "json_object"}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("llm: encode request: %w", err)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	url := strings.TrimRight(c.BaseURL, "/")
	if strings.HasSuffix(url, "/chat/completions") == false {
		url += "/chat/completions"
	}
	log.Println("LLM Request=", string(body))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("llm: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if c.Organization != "" {
		httpReq.Header.Set("OpenAI-Organization", c.Organization)
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		log.Println(err)
		return CompletionResponse{}, fmt.Errorf("llm: request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		log.Println(err)
		return CompletionResponse{}, fmt.Errorf("llm: read response: %w", err)
	}
	log.Println("LLM Response=", string(data))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CompletionResponse{}, fmt.Errorf("llm: HTTP %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var decoded completionResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return CompletionResponse{}, fmt.Errorf("llm: decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("llm: response contained no choices")
	}
	return CompletionResponse{Content: decoded.Choices[0].Message.Content, PromptTokens: decoded.Usage.PromptTokens, OutputTokens: decoded.Usage.CompletionTokens, TotalTokens: decoded.Usage.TotalTokens}, nil
}
