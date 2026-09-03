// Package llm provides a small provider-neutral interface for chat models.
package llm

import (
	"context"
	"encoding/json"
)

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

	// ResponseFormat forces the model to emit a well-formed JSON object
	// (e.g. {"type": "json_object"} or {"type": "json_schema", ...}) so the
	// completion can be unmarshaled consistently. Empty disables the constraint.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// ResponseFormat is a JSON-schema or type constraint used to force structured
// JSON output from OpenAI-compatible chat completions endpoints.
//
// The payload follows OpenAI's response_format convention:
//
//	{"type": "json_object"}                                     // any JSON object
//	{"type": "json_schema", "json_schema": {...}}               // structured
//	{"type": "json_schema", "json_schema": {"strict": true}}    // strict
type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema is the JSON Schema definition passed inside response_format for
// structured output. Name is required by some APIs for referencing the schema.
type JSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`

	// Strict constrains the model to conform exactly to the schema, following
	// OpenAI's structured-outputs convention (inside json_schema).
	Strict *bool `json:"strict,omitempty"`
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
