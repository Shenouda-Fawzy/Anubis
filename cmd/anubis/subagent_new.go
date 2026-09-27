package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type AgentCard struct {
	Name        string // Optional
	Description string // Mandatory
	Model       string // Optional
}

// ChatCompleter is the LLM dependency shared by Agent and Coordinator.
// *OpenAIClient satisfies it; tests can substitute a stub.
type ChatCompleter interface {
	ModelName() string
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

type Agent struct {
	*AgentCard
	LlmClient    ChatCompleter
	Finding      string
	ReviewStatus ReviewStatus
}

type ReviewStatus int

const (
	StatusReviewCompleted  ReviewStatus = 200
	StatusReviewInComplete ReviewStatus = 300
)

type ReviewResult struct {
	Diff              string
	ReviewFindingText string // Free-form text
	ReviewStatus      ReviewStatus
}

func (r *ReviewResult) Reviewed() bool {
	if r == nil {
		slog.Warn("review result is nil")
		return false
	}
	return r.ReviewStatus == StatusReviewCompleted
}

type ReviewRequest struct {
	RepoName               string
	PullRequestTitle       string
	PullRequestDescription string
	PullReqNumber          int
	Diff                   string
}

// name is a nil-safe accessor for the agent's display name.
func (a *Agent) name() string {
	if a == nil || a.AgentCard == nil || a.Name == "" {
		return "N/A"
	}
	return a.Name
}

// Review runs this agent over the pull request and stores its raw findings in
// Finding. It returns an error only when the agent could not be run at all; an
// agent that legitimately finds nothing returns nil with an empty Finding.
func (a *Agent) Review(ctx context.Context, pr *ReviewRequest) error {
	name := a.name()
	slog.Debug("sub-agent started", "agent", name)
	defer slog.Debug("sub-agent done", "agent", name)

	if a == nil || a.AgentCard == nil {
		return errors.New("invalid agent")
	}
	if a.LlmClient == nil {
		return errors.New("invalid llm client")
	}
	if pr == nil {
		return errors.New("invalid pull request")
	}
	if pr.Diff == "" {
		return fmt.Errorf("empty diff for pull request #%d", pr.PullReqNumber)
	}
	c := NewCompletionRequest(a.Model, subAgentPrompt, subAgentTask(pr, a.Description))
	if c == nil {
		return errors.New("unable to create completion request: model is not set")
	}
	result, err := a.LlmClient.Complete(ctx, c)
	if err != nil {
		slog.Error("sub-agent completion failed", "agent", name, "error", err)
		return err
	}
	if result == nil {
		return errors.New("llm returned nil completion response")
	}
	a.Finding = result.Content
	if result.Completed() {
		a.ReviewStatus = StatusReviewCompleted
	} else {
		a.ReviewStatus = StatusReviewInComplete
		slog.Warn("sub-agent output was truncated", "agent", name, "finish_reason", result.FinishReason)
	}
	slog.Debug("sub-agent result", "agent", name, "bytes", len(result.Content), "finish_reason", result.FinishReason)
	return nil
}
