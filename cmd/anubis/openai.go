package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

func NewOpenAIClient(apiKey, baseURL, model string) *OpenAIClient {
	return &OpenAIClient{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, HTTPClient: http.DefaultClient}
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
	Code    string `json:"code"`
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
	if o.APIKey == "" || o.BaseURL == "" {
		return nil, errors.New("invalid OpenAIClient ensure missing API key or base url")
	}
	url := strings.TrimRight(o.BaseURL, "/")
	if strings.HasSuffix(url, "/chat/completions") == false {
		url += "/chat/completions"
	}
	body, err := json.Marshal(req)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	fmt.Printf("API POST %s", url)
	fmt.Println("*** Req Body ***")
	fmt.Println(string(body))
	fmt.Println("*** End Req body ***")
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
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 30<<20))
	if err != nil {
		log.Println(err)
		return nil, err
	}
	fmt.Println("*** Resp Body ***")
	fmt.Println(string(data))
	fmt.Println("*** End Resp body ***")
	var d *completionResponse
	err = json.Unmarshal(data, &d)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if d.HasError() {
		log.Println("completion finished with error")
		return nil, fmt.Errorf("error message=%s, code=%s, type=%s, param=%s", d.Err.Message, d.Err.Code, d.Err.Type, d.Err.Param)
	}
	cr := CompletionResponse{
		Content:      d.TextContent(),
		FinishReason: d.FinishReason(),
	}
	return &cr, nil
}
