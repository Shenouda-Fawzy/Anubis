package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAgent(t *testing.T, srv *httptest.Server) *Agent {
	t.Helper()
	client := NewOpenAIClient("test-key", srv.URL, "test-model")
	client.HTTPClient = srv.Client()
	return &Agent{
		AgentCard: &AgentCard{
			Name:        "test-agent",
			Description: "review the diff",
			Model:       "test-model",
		},
		LlmClient: client,
	}
}

func TestAgentReviewSuccess(t *testing.T) {
	const finding = `found a nil-pointer dereference near line 42`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization header = %q, want %q", got, "Bearer test-key")
		}
		var req CompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Model != "test-model" {
			t.Errorf("model = %q, want %q", req.Model, "test-model")
		}
		if len(req.Messages) != 2 {
			t.Fatalf("messages = %d, want 2", len(req.Messages))
		}
		if req.Messages[0].Role != "system" || req.Messages[0].Content != masterPrompt {
			t.Error("system message does not contain masterPrompt")
		}
		if req.Messages[1].Role != "user" || req.Messages[1].Content != "review the diff" {
			t.Error("user message does not carry the agent description")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{}}`, finding) //nolint
	}))
	defer srv.Close()

	agent := testAgent(t, srv)
	if err := agent.Review(context.Background(), &ReviewRequest{}); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if agent.Finding != finding {
		t.Errorf("Finding = %q, want %q", agent.Finding, finding)
	}
	if agent.ReviewStatus != StatusReviewCompleted {
		t.Errorf("ReviewStatus = %d, want %d", agent.ReviewStatus, StatusReviewCompleted)
	}
}

func TestAgentReviewIncomplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}],"usage":{}}`) //nolint
	}))
	defer srv.Close()

	agent := testAgent(t, srv)
	if err := agent.Review(context.Background(), &ReviewRequest{}); err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if agent.ReviewStatus != StatusReviewInComplete {
		t.Errorf("ReviewStatus = %d, want %d", agent.ReviewStatus, StatusReviewInComplete)
	}
}

func TestAgentReviewAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":{"message":"boom","type":"server_error","param":"","code":500}}`) //nolint
	}))
	defer srv.Close()

	agent := testAgent(t, srv)
	err := agent.Review(context.Background(), &ReviewRequest{})
	if err == nil {
		t.Fatal("Review() error = nil, want API error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("Review() error = %q, want it to contain %q", err, "boom")
	}
}

func TestAgentReviewInvalidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "not json") //nolint
	}))
	defer srv.Close()

	agent := testAgent(t, srv)
	if err := agent.Review(context.Background(), &ReviewRequest{}); err == nil {
		t.Fatal("Review() error = nil, want parse error")
	}
}

func TestAgentReviewContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	agent := testAgent(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := agent.Review(ctx, &ReviewRequest{}); err == nil {
		t.Fatal("Review() error = nil, want context-cancelled error")
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
		LlmClient: NewOpenAIClient("test-key", "http://localhost:1", "test-model"),
	}
	err := agent.Review(context.Background(), &ReviewRequest{})
	if err == nil || !strings.Contains(err.Error(), "unable to create completion request") {
		t.Fatalf("Review() error = %v, want %q", err, "unable to create completion request")
	}
}

func TestAgentReviewNilLlmClient(t *testing.T) {
	agent := &Agent{
		AgentCard: &AgentCard{
			Name:        "test-agent",
			Description: "review the diff",
			Model:       "test-model",
		},
	}
	err := agent.Review(context.Background(), &ReviewRequest{})
	if err == nil {
		t.Fatal("Review() error = nil, want nil-client error")
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
