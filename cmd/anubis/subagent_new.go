package main

import (
	"context"
	"errors"
	"log/slog"
)

type AgentStatus int

const (
	StatusInactive AgentStatus = 0
	StatusActive   AgentStatus = 1
)

type AgentCard struct {
	Name        string // Optional
	Status      int    // Optional (default: active)
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

func (a *Agent) Done() {
	if a != nil {
		a.AgentCard = nil
		a.Description = ""
		a.Finding = ""
		a.Model = ""
		a.Name = ""
		a.LlmClient = nil
		a = nil
	}
}

func (a *Agent) Review(ctx context.Context, pr *ReviewRequest) error {
	name := ""
	if a != nil && a.AgentCard != nil {
		name = a.Name
	}
	if name == "" {
		name = "N/A"
	}
	slog.Debug("Subagent started", "agent", name)
	defer slog.Debug("Subagent done", "agent", name)

	if a == nil || a.AgentCard == nil {
		return errors.New("invalid agent")
	}
	if a.LlmClient == nil {
		return errors.New("invalid llm client")
	}
	c := NewCompletionRequest(a.Model, masterPrompt, a.Description)
	if c == nil {
		return errors.New("unable to create completion request")
	}
	result, err := a.LlmClient.Complete(ctx, c)
	if err != nil {
		slog.Error("subagent completion failed", "agent", name, "error", err)
		return err
	}
	slog.Debug("Subagent Result", "agent", name, "content", result.Content, "finish_reason", result.FinishReason)
	a.Finding = result.Content
	if result.Completed() {
		a.ReviewStatus = StatusReviewCompleted
	} else {
		a.ReviewStatus = StatusReviewInComplete
	}
	return nil
}
