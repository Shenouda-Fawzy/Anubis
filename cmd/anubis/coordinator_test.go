package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
	if c.result.Reviewed() != true {
		t.Error("ReviewResult.Reviewed() = false, want true")
	}
}

func TestCoordinatorReviewNoAgents(t *testing.T) {
	llm := &fakeCompleter{}
	c := NewCoordinator(nil, "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1}, "diff")

	err := c.Review(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no review agents configured") {
		t.Fatalf("Review() error = %v, want a no-agents error", err)
	}
}

func TestCoordinatorNil(t *testing.T) {
	var c *Coordinator
	if err := c.Review(context.Background()); err == nil {
		t.Fatal("Review() on nil coordinator returned nil error")
	}
	if c.FindingsText() != "" {
		t.Error("FindingsText() on nil coordinator should be empty")
	}
	if c.Failures() != nil {
		t.Error("Failures() on nil coordinator should be nil")
	}
	if c.DiffTruncated() {
		t.Error("DiffTruncated() on nil coordinator should be false")
	}
}

// Every template verb must be filled by a distinct field. A short argument list
// silently shifts values and renders %!s(MISSING) into the prompt, which is
// exactly the class of bug this test exists to prevent.
func TestReviewPromptArgumentsAreAligned(t *testing.T) {
	llm := &recordingCompleter{finding: "f", synthesis: "final"}
	c := NewCoordinator(testAgents(llm), "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 4242, Title: "TITLE", Description: "DESCRIPTION"}, "THE_DIFF")

	if err := c.Review(context.Background()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	sent := llm.synthesisPrompt()
	if strings.Contains(sent, "%!") {
		t.Fatalf("synthesis prompt contains a formatting error:\n%s", sent)
	}
	for _, want := range []string{
		"<repository>acme/widgets</repository>",
		"<pull_request>4242</pull_request>",
		"<title>TITLE</title>",
		"DESCRIPTION",
		"THE_DIFF",
		"<reviewer-finding>",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("synthesis prompt is missing %q", want)
		}
	}
}

func TestCoordinatorRunsAgentsConcurrently(t *testing.T) {
	const agents = 4
	llm := &recordingCompleter{finding: "f", synthesis: "final", delay: 150 * time.Millisecond}

	as := make([]*Agent, agents)
	for i := range as {
		as[i] = &Agent{
			AgentCard: &AgentCard{Name: fmt.Sprintf("a%d", i), Description: "look", Model: "test-model"},
			LlmClient: llm,
		}
	}
	c := NewCoordinator(as, "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1}, "diff")

	start := time.Now()
	if err := c.Review(context.Background()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	elapsed := time.Since(start)

	// One synthesis call plus four agent calls, each gated behind the delay.
	if elapsed >= agents*llm.delay {
		t.Errorf("review took %v; agents look serial (want roughly one delay, not %d of them)", elapsed, agents)
	}
	if got := llm.agentCount(); got != agents {
		t.Errorf("agent completions = %d, want %d", got, agents)
	}
}

// Findings must reach the synthesis prompt in configured agent order so output
// is stable regardless of which agent finishes first.
func TestCoordinatorPreservesAgentOrder(t *testing.T) {
	llm := &recordingCompleter{synthesis: "final"}
	names := []string{"zeta", "alpha", "middle"}
	as := make([]*Agent, len(names))
	for i, n := range names {
		as[i] = &Agent{
			AgentCard: &AgentCard{Name: n, Description: "look", Model: "test-model"},
			LlmClient: llm,
		}
	}
	c := NewCoordinator(as, "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1}, "diff")

	if err := c.Review(context.Background()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	sent := llm.synthesisPrompt()
	prev := -1
	for _, n := range names {
		i := strings.Index(sent, "Sub-agent reviewer name: "+n)
		if i < 0 {
			t.Fatalf("agent %q is missing from the synthesis prompt:\n%s", n, sent)
		}
		if i < prev {
			t.Errorf("agent %q appears out of order in the synthesis prompt", n)
		}
		prev = i
	}
}

// One flaky specialist must not discard the work of the others, and the review
// must disclose the reduced coverage.
func TestCoordinatorToleratesPartialAgentFailure(t *testing.T) {
	llm := &recordingCompleter{synthesis: "final", failOn: map[string]bool{"SECURITY_FOCUS": true}}
	as := []*Agent{
		{AgentCard: &AgentCard{Name: "security", Description: "SECURITY_FOCUS", Model: "test-model"}, LlmClient: llm},
		{AgentCard: &AgentCard{Name: "performance", Description: "PERF_FOCUS", Model: "test-model"}, LlmClient: llm},
	}
	c := NewCoordinator(as, "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1}, "diff")

	if err := c.Review(context.Background()); err != nil {
		t.Fatalf("Review() error = %v, want the review to survive one agent failure", err)
	}
	if c.FindingsText() != "final" {
		t.Errorf("FindingsText() = %q, want %q", c.FindingsText(), "final")
	}
	f := c.Failures()
	if len(f) != 1 {
		t.Fatalf("Failures() = %v, want exactly one", f)
	}
	if f[0].Name != "security" {
		t.Errorf("failed agent = %q, want %q", f[0].Name, "security")
	}
	if got := caveats(c); !strings.Contains(got, "1 of 2 review agents failed (security)") {
		t.Errorf("caveats = %q, want it to disclose the failed agent", got)
	}
}

func TestCoordinatorFailsWhenEveryAgentFails(t *testing.T) {
	llm := &recordingCompleter{failOn: map[string]bool{"A_FOCUS": true, "B_FOCUS": true}}
	as := []*Agent{
		{AgentCard: &AgentCard{Name: "a", Description: "A_FOCUS", Model: "test-model"}, LlmClient: llm},
		{AgentCard: &AgentCard{Name: "b", Description: "B_FOCUS", Model: "test-model"}, LlmClient: llm},
	}
	c := NewCoordinator(as, "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1}, "diff")

	err := c.Review(context.Background())
	if err == nil {
		t.Fatal("Review() error = nil, want an error when no agent produced a finding")
	}
	if !strings.Contains(err.Error(), "all 2 review agents failed") {
		t.Errorf("Review() error = %q, want it to name the total failure", err)
	}
}

func TestTruncateDiff(t *testing.T) {
	small := strings.Repeat("x", maxDiffBytes)
	if got := truncateDiff(small); got != small {
		t.Error("a diff under the limit must be passed through unchanged")
	}

	big := strings.Repeat("line of diff\n", maxDiffBytes/10+10)
	got := truncateDiff(big)
	if len(got) >= len(big) {
		t.Errorf("truncated length = %d, want less than the original %d", len(got), len(big))
	}
	if !strings.Contains(got, "diff truncated") {
		t.Error("truncated diff is missing its marker")
	}
	if strings.HasSuffix(strings.TrimSpace(got), "diff of a file\nd") {
		t.Error("truncation did not land on a line boundary")
	}
}

func TestCaveatsAreEmptyOnACleanReview(t *testing.T) {
	llm := &fakeCompleter{result: &CompletionResponse{Content: "clean", FinishReason: "stop"}}
	c := NewCoordinator(testAgents(llm), "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1}, "diff")
	if err := c.Review(context.Background()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if got := caveats(c); got != "" {
		t.Errorf("caveats = %q, want empty", got)
	}
}

func TestCaveatsDiscloseTruncatedDiff(t *testing.T) {
	llm := &fakeCompleter{result: &CompletionResponse{Content: "clean", FinishReason: "stop"}}
	c := NewCoordinator(testAgents(llm), "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1}, strings.Repeat("line\n", maxDiffBytes/4+10))
	if err := c.Review(context.Background()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if got := caveats(c); !strings.Contains(got, "truncated diff") {
		t.Errorf("caveats = %q, want a truncation disclosure", got)
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

// recordingCompleter returns a distinct finding per agent and captures the
// synthesis prompt, so tests can assert on what the coordinator actually sent.
type recordingCompleter struct {
	finding   string
	synthesis string
	delay     time.Duration
	failOn    map[string]bool

	mu       sync.Mutex
	requests []*CompletionRequest
	agents   atomic.Int32
}

func (r *recordingCompleter) ModelName() string { return "test-model" }

func (r *recordingCompleter) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	user := req.Messages[len(req.Messages)-1].Content

	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()

	if strings.Contains(user, "<reviewer_findings>") {
		return &CompletionResponse{Content: r.synthesis, FinishReason: "stop"}, nil
	}
	r.agents.Add(1)
	focus := focusFromTask(user)
	if r.failOn[focus] {
		return nil, errors.New("boom")
	}
	return &CompletionResponse{Content: r.finding + ":" + focus, FinishReason: "stop"}, nil
}

func (r *recordingCompleter) agentCount() int { return int(r.agents.Load()) }

// synthesisPrompt returns the user message sent for the coordinator call.
func (r *recordingCompleter) synthesisPrompt() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, req := range r.requests {
		if strings.Contains(req.Messages[len(req.Messages)-1].Content, "<reviewer_findings>") {
			return req.Messages[len(req.Messages)-1].Content
		}
	}
	return ""
}

// focusFromTask returns the text the agent received under "# Your focus", which
// is how these tests tell the specialists apart.
func focusFromTask(task string) string {
	_, rest, found := strings.Cut(task, "# Your focus")
	if !found {
		return ""
	}
	focus, _, _ := strings.Cut(rest, "# Pull Request")
	return strings.TrimSpace(focus)
}

// A provider that answers with an empty completion must never be mistaken for a
// clean review. This used to panic on an empty failure list.
func TestCoordinatorEmptyProviderOutputIsAFailure(t *testing.T) {
	llm := &emptyCompleter{}
	c := NewCoordinator([]*Agent{
		{AgentCard: &AgentCard{Name: "a", Description: "A_FOCUS", Model: "test-model"}, LlmClient: llm},
		{AgentCard: &AgentCard{Name: "b", Description: "B_FOCUS", Model: "test-model"}, LlmClient: llm},
	}, "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1}, "diff")

	err := c.Review(context.Background())
	if err == nil {
		t.Fatal("Review() error = nil, want an error when every agent returns nothing")
	}
	if !strings.Contains(err.Error(), "all 2 review agents failed") {
		t.Errorf("Review() error = %q, want it to report every agent failed", err)
	}
	if c.FindingsText() != "" {
		t.Errorf("FindingsText() = %q, want empty", c.FindingsText())
	}
}

// One agent returning nothing and another returning a finding: the review must
// still be produced, with the empty one disclosed.
func TestCoordinatorMixedEmptyAndRealFindings(t *testing.T) {
	llm := &oneEmptyCompleter{}
	c := NewCoordinator([]*Agent{
		{AgentCard: &AgentCard{Name: "quiet", Description: "A_FOCUS", Model: "test-model"}, LlmClient: llm},
		{AgentCard: &AgentCard{Name: "loud", Description: "B_FOCUS", Model: "test-model"}, LlmClient: llm},
	}, "acme/widgets", llm)
	c.SetPRdetails(&PullRequest{Number: 1}, "diff")

	if err := c.Review(context.Background()); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	f := c.Failures()
	if len(f) != 1 || f[0].Name != "quiet" {
		t.Fatalf("Failures() = %v, want just the quiet agent", f)
	}
	if got := caveats(c); !strings.Contains(got, "1 of 2 review agents failed (quiet)") {
		t.Errorf("caveats = %q, want the empty agent disclosed", got)
	}
}

type emptyCompleter struct{}

func (emptyCompleter) ModelName() string { return "test-model" }
func (emptyCompleter) Complete(context.Context, *CompletionRequest) (*CompletionResponse, error) {
	return &CompletionResponse{Content: "", FinishReason: "stop"}, nil
}

type oneEmptyCompleter struct{}

func (oneEmptyCompleter) ModelName() string { return "test-model" }
func (oneEmptyCompleter) Complete(_ context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	user := req.Messages[len(req.Messages)-1].Content
	if strings.Contains(user, "A_FOCUS") {
		return &CompletionResponse{Content: "", FinishReason: "stop"}, nil
	}
	return &CompletionResponse{Content: "real finding", FinishReason: "stop"}, nil
}
