// Package llm provides a small provider-neutral interface for chat models.
package llm

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionRequest struct {
	Model       string    `json:"model,omitempty"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`

	// Dictates how many tokens the model can return to you as visible text output
	MaxTokens int `json:"max_tokens,omitempty"`
}

type CompletionResponse struct {
	Content      string
	PromptTokens int
	OutputTokens int
	TotalTokens  int
}

// Client is intentionally compatible with OpenAI-style chat APIs, while
// allowing local and test providers to be used without special cases.
type Client interface {
	Complete(context.Context, CompletionRequest) (CompletionResponse, error)
}

// LLM is a descriptive alias for Client.
type LLM = Client
