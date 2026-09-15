package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeCompleter struct {
	result *CompletionResponse
	err    error
	gotReq *CompletionRequest
	ctx    context.Context
}

func (f *fakeCompleter) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	f.ctx = ctx
	f.gotReq = req
	return f.result, f.err
}

func (f *fakeCompleter) ModelName() string { return "test-model" }

func testAgent(llm ChatCompleter) *Agent {
	return &Agent{
		AgentCard: &AgentCard{
			Name:        "test-agent",
			Description: "review the diff",
			Model:       "test-model",
		},
		LlmClient: llm,
	}
}

func TestAgentReviewSuccess(t *testing.T) {
	const finding = `found a nil-pointer dereference near line 42`
	llm := &fakeCompleter{result: &CompletionResponse{Content: finding, FinishReason: "stop"}}
	agent := testAgent(llm)

	if err := agent.Review(context.Background(), &ReviewRequest{}); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if agent.Finding != finding {
		t.Errorf("Finding = %q, want %q", agent.Finding, finding)
	}
	if agent.ReviewStatus != StatusReviewCompleted {
		t.Errorf("ReviewStatus = %d, want %d", agent.ReviewStatus, StatusReviewCompleted)
	}
	if llm.gotReq == nil {
		t.Fatal("request was not sent to the client")
	}
	if llm.gotReq.Model != "test-model" {
		t.Errorf("request model = %q, want %q", llm.gotReq.Model, "test-model")
	}
	if len(llm.gotReq.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(llm.gotReq.Messages))
	}
	if llm.gotReq.Messages[0].Role != "system" || llm.gotReq.Messages[0].Content != masterPrompt {
		t.Error("system message does not contain masterPrompt")
	}
	if llm.gotReq.Messages[1].Role != "user" || llm.gotReq.Messages[1].Content != "review the diff" {
		t.Error("user message does not carry the agent description")
	}
}

func TestAgentReviewIncomplete(t *testing.T) {
	llm := &fakeCompleter{result: &CompletionResponse{Content: "partial", FinishReason: "length"}}
	agent := testAgent(llm)

	if err := agent.Review(context.Background(), &ReviewRequest{}); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if agent.ReviewStatus != StatusReviewInComplete {
		t.Errorf("ReviewStatus = %d, want %d", agent.ReviewStatus, StatusReviewInComplete)
	}
}

func TestAgentReviewLLMError(t *testing.T) {
	llm := &fakeCompleter{err: errors.New("boom")}
	agent := testAgent(llm)

	err := agent.Review(context.Background(), &ReviewRequest{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Review() error = %v, want it to contain %q", err, "boom")
	}
	if agent.ReviewStatus != 0 {
		t.Errorf("ReviewStatus = %d, want 0 on error", agent.ReviewStatus)
	}
}

func TestAgentReviewNoContent(t *testing.T) {
	llm := &fakeCompleter{result: &CompletionResponse{}}
	agent := testAgent(llm)

	if err := agent.Review(context.Background(), &ReviewRequest{}); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if agent.Finding != "" {
		t.Errorf("Finding = %q, want empty", agent.Finding)
	}
	if agent.ReviewStatus != StatusReviewInComplete {
		t.Errorf("ReviewStatus = %d, want %d", agent.ReviewStatus, StatusReviewInComplete)
	}
}

func TestAgentReviewForwardsContext(t *testing.T) {
	llm := &fakeCompleter{result: &CompletionResponse{Content: "ok", FinishReason: "stop"}}
	agent := testAgent(llm)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := agent.Review(ctx, &ReviewRequest{}); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if llm.ctx == nil {
		t.Fatal("context was not forwarded to the client")
	}
	if err := llm.ctx.Err(); err == nil {
		t.Error("client received a live context, want the cancelled context")
	}
}

func TestAgentReviewNilAgent(t *testing.T) {
	var agent *Agent
	err := agent.Review(context.Background(), &ReviewRequest{})
	if err == nil || !strings.Contains(err.Error(), "invalid agent") {
		t.Fatalf("Review() error = %v, want %q", err, "invalid agent")
	}
}

func TestAgentReviewNilAgentCard(t *testing.T) {
	agent := &Agent{}
	err := agent.Review(context.Background(), &ReviewRequest{})
	if err == nil || !strings.Contains(err.Error(), "invalid agent") {
		t.Fatalf("Review() error = %v, want %q", err, "invalid agent")
	}
}

func TestAgentReviewMissingModel(t *testing.T) {
	agent := &Agent{
		AgentCard: &AgentCard{Description: "review the diff"},
		LlmClient: &fakeCompleter{},
	}
	err := agent.Review(context.Background(), &ReviewRequest{})
	if err == nil || !strings.Contains(err.Error(), "unable to create completion request") {
		t.Fatalf("Review() error = %v, want %q", err, "unable to create completion request")
	}
}

func TestAgentReviewNilLlmClient(t *testing.T) {
	agent := testAgent(nil)
	err := agent.Review(context.Background(), &ReviewRequest{})
	if err == nil || !strings.Contains(err.Error(), "invalid llm client") {
		t.Fatalf("Review() error = %v, want %q", err, "invalid llm client")
	}
}

func TestReviewed(t *testing.T) {
	cases := []struct {
		name string
		r    *ReviewResult
		want bool
	}{
		{"nil", nil, false},
		{"zero value", &ReviewResult{}, false},
		{"completed", &ReviewResult{ReviewStatus: StatusReviewCompleted}, true},
		{"incomplete", &ReviewResult{ReviewStatus: StatusReviewInComplete}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Reviewed(); got != tc.want {
				t.Errorf("Reviewed() = %v, want %v", got, tc.want)
			}
		})
	}
}
