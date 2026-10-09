package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type fakeCompleter struct {
	result *CompletionResponse
	err    error

	mu     sync.Mutex
	gotReq *CompletionRequest
	ctx    context.Context
}

func (f *fakeCompleter) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	f.mu.Lock()
	f.ctx = ctx
	f.gotReq = req
	f.mu.Unlock()
	return f.result, f.err
}

func (f *fakeCompleter) request() *CompletionRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotReq
}

func (f *fakeCompleter) context() context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ctx
}

// testPR is a minimal pull request with a non-empty diff, which every agent
// review requires.
func testPR() *ReviewRequest {
	return &ReviewRequest{
		RepoName:               "acme/widgets",
		PullReqNumber:          42,
		PullRequestTitle:       "Add users endpoint",
		PullRequestDescription: "Introduces a public users endpoint.",
		Diff:                   "diff --git a/server.go b/server.go\n+handleUsers()\n",
	}
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

	if err := agent.Review(context.Background(), testPR()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if agent.Finding != finding {
		t.Errorf("Finding = %q, want %q", agent.Finding, finding)
	}
	if agent.ReviewStatus != StatusReviewCompleted {
		t.Errorf("ReviewStatus = %d, want %d", agent.ReviewStatus, StatusReviewCompleted)
	}
	req := llm.request()
	if req == nil {
		t.Fatal("request was not sent to the client")
	}
	if req.Model != "test-model" {
		t.Errorf("request model = %q, want %q", req.Model, "test-model")
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != subAgentPrompt {
		t.Error("system message does not carry the specialist prompt")
	}
	if req.Messages[0].Content == masterPrompt {
		t.Error("sub-agent was given the coordinator prompt")
	}
	user := req.Messages[1].Content
	if req.Messages[1].Role != "user" {
		t.Errorf("user message role = %q, want %q", req.Messages[1].Role, "user")
	}
	if !strings.Contains(user, "review the diff") {
		t.Error("user message does not carry the agent description")
	}
}

// An agent with its own prompt must send that prompt as the system message
// rather than the generic specialist fallback. This is how the embedded
// Markdown specialists reach the model.
func TestAgentReviewUsesAgentSystemPrompt(t *testing.T) {
	const agentPrompt = "# Security Agent\n\nYou are a senior application-security engineer."
	llm := &fakeCompleter{result: &CompletionResponse{Content: "ok", FinishReason: "stop"}}
	agent := testAgent(llm)
	agent.SystemPrompt = agentPrompt

	if err := agent.Review(context.Background(), testPR()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	system := llm.request().Messages[0].Content
	if system != agentPrompt {
		t.Errorf("system message = %q, want the agent's own prompt", system)
	}
	if system == subAgentPrompt {
		t.Error("agent with a system prompt fell back to the generic specialist prompt")
	}
}

// The specialist must actually see the change it is reviewing.
func TestAgentReviewSendsDiffAndPRContext(t *testing.T) {
	llm := &fakeCompleter{result: &CompletionResponse{Content: "ok", FinishReason: "stop"}}
	agent := testAgent(llm)

	if err := agent.Review(context.Background(), testPR()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	user := llm.request().Messages[1].Content
	for _, want := range []string{"acme/widgets", "42", "Add users endpoint", "Introduces a public users endpoint.", "handleUsers()"} {
		if !strings.Contains(user, want) {
			t.Errorf("user message is missing %q\n%s", want, user)
		}
	}
}

func TestAgentReviewRejectsEmptyDiff(t *testing.T) {
	llm := &fakeCompleter{result: &CompletionResponse{Content: "ok", FinishReason: "stop"}}
	agent := testAgent(llm)

	err := agent.Review(context.Background(), &ReviewRequest{PullReqNumber: 7})
	if err == nil || !strings.Contains(err.Error(), "empty diff") {
		t.Fatalf("Review() error = %v, want an empty-diff error", err)
	}
	if llm.request() != nil {
		t.Error("a request was sent despite the empty diff")
	}
}

func TestAgentReviewNilPullRequest(t *testing.T) {
	agent := testAgent(&fakeCompleter{})
	err := agent.Review(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "invalid pull request") {
		t.Fatalf("Review() error = %v, want %q", err, "invalid pull request")
	}
}

func TestAgentReviewNilResponse(t *testing.T) {
	agent := testAgent(&fakeCompleter{result: nil})
	err := agent.Review(context.Background(), testPR())
	if err == nil || !strings.Contains(err.Error(), "nil completion response") {
		t.Fatalf("Review() error = %v, want a nil-response error", err)
	}
}

func TestAgentReviewIncomplete(t *testing.T) {
	llm := &fakeCompleter{result: &CompletionResponse{Content: "partial", FinishReason: "length"}}
	agent := testAgent(llm)

	if err := agent.Review(context.Background(), testPR()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if agent.ReviewStatus != StatusReviewInComplete {
		t.Errorf("ReviewStatus = %d, want %d", agent.ReviewStatus, StatusReviewInComplete)
	}
}

func TestAgentReviewLLMError(t *testing.T) {
	llm := &fakeCompleter{err: errors.New("boom")}
	agent := testAgent(llm)

	err := agent.Review(context.Background(), testPR())
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

	if err := agent.Review(context.Background(), testPR()); err != nil {
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
	if err := agent.Review(ctx, testPR()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if got := llm.context(); got == nil {
		t.Fatal("context was not forwarded to the client")
	} else if err := got.Err(); err == nil {
		t.Error("client received a live context, want the cancelled context")
	}
}

func TestAgentReviewNilAgent(t *testing.T) {
	var agent *Agent
	err := agent.Review(context.Background(), testPR())
	if err == nil || !strings.Contains(err.Error(), "invalid agent") {
		t.Fatalf("Review() error = %v, want %q", err, "invalid agent")
	}
}

func TestAgentReviewNilAgentCard(t *testing.T) {
	agent := &Agent{}
	err := agent.Review(context.Background(), testPR())
	if err == nil || !strings.Contains(err.Error(), "invalid agent") {
		t.Fatalf("Review() error = %v, want %q", err, "invalid agent")
	}
}

func TestAgentReviewMissingModel(t *testing.T) {
	agent := &Agent{
		AgentCard: &AgentCard{Description: "review the diff"},
		LlmClient: &fakeCompleter{},
	}
	err := agent.Review(context.Background(), testPR())
	if err == nil || !strings.Contains(err.Error(), "unable to create completion request") {
		t.Fatalf("Review() error = %v, want %q", err, "unable to create completion request")
	}
}

func TestAgentReviewNilLlmClient(t *testing.T) {
	agent := testAgent(nil)
	err := agent.Review(context.Background(), testPR())
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
