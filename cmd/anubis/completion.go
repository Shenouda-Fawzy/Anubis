package main

// Originally 'llm' pkg

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionRequest struct {
	Model    string     `json:"model,omitempty"`
	Messages []*Message `json:"messages"`
}

type CompletionResponse struct {
	Content      string
	FinishReason string
	PromptTokens int
	OutputTokens int
	TotalTokens  int
}

// Tokens reports the provider-reported token counts for the call. They are zero
// when the provider omits usage.
func (c *CompletionResponse) Tokens() (prompt, completion, total int) {
	if c == nil {
		return 0, 0, 0
	}
	return c.PromptTokens, c.OutputTokens, c.TotalTokens
}

func (c *CompletionResponse) Completed() bool {
	if c == nil {
		return false
	}
	return c.FinishReason == "stop"
}

func NewCompletionRequest(model, instructions, userPrompt string) *CompletionRequest {
	if model == "" || instructions == "" || userPrompt == "" {
		return nil
	}
	return &CompletionRequest{
		Model: model,
		Messages: []*Message{
			{Role: "system", Content: instructions},
			{Role: "user", Content: userPrompt},
		},
	}
}
