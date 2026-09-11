package main

import (
	"context"
	"errors"
	"log"
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

type Agent struct {
	*AgentCard
	LlmClient    *OpenAIClient
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
		log.Println("r = nil")
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
	log.Println("Subagent started")
	defer log.Println("Subagent done")

	if a == nil || a.AgentCard == nil {
		return errors.New("invalid agent")
	}
	c := NewCompletionRequest(a.Model, masterPrompt, a.Description)
	if c == nil {
		return errors.New("unable to create completion request")
	}
	result, err := a.LlmClient.Complete(ctx, c)
	if err != nil {
		log.Println(err)
		return err
	}
	log.Printf("Subagent Result = %#v\n", result)
	a.Finding = result.Content
	if result.Completed() {
		a.ReviewStatus = StatusReviewCompleted
	} else {
		a.ReviewStatus = StatusReviewInComplete
	}
	return nil
}
