package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCoordinatorReviewLLMError(t *testing.T) {
	llm := &fakeCompleter{err: errors.New("rate limited: 429 Too Many Requests")}
	c := NewCoordinator(testAgents(llm), "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1, Title: "t", Description: "d"}, "diff")

	err := c.Review(context.Background())
	if err == nil {
		t.Fatal("Review() error = nil, want LLM error")
	}
	if strings.Contains(err.Error(), "rate limited") == false {
		t.Errorf("Review() error = %q, want it to contain the LLM error", err)
	}
	if c.FindingsText() != "" {
		t.Errorf("FindingsText() = %q, want empty on failure", c.FindingsText())
	}

	comment := failureComment(err)
	if strings.Contains(comment, "could not complete the review") == false {
		t.Errorf("failure comment = %q, want descriptive failure notice", comment)
	}
	if strings.Contains(comment, "rate limited") == false {
		t.Errorf("failure comment = %q, want it to embed the LLM error", comment)
	}
}

func TestCoordinatorReviewSuccessKeepsFindings(t *testing.T) {
	llm := &fakeCompleter{result: &CompletionResponse{Content: "all good", FinishReason: "stop"}}
	c := NewCoordinator(testAgents(llm), "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1, Title: "t", Description: "d"}, "diff")

	if err := c.Review(context.Background()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if c.FindingsText() != "all good" {
		t.Errorf("FindingsText() = %q, want %q", c.FindingsText(), "all good")
	}
}

func testAgents(llm ChatCompleter) []*Agent {
	return []*Agent{
		{
			AgentCard: &AgentCard{Name: "security", Description: "authentication, authorization, injection", Model: "test-model"},
			LlmClient: llm,
		},
	}
}
