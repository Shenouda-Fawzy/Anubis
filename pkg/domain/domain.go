// Package domain contains the types shared by Anubis adapters and services.
package domain

import "context"

// Severity is the impact of a finding.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Finding is a single actionable observation about a pull request.
type Finding struct {
	ID          string   `json:"id,omitempty"`
	Agent       string   `json:"agent,omitempty"`
	Severity    Severity `json:"severity"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	File        string   `json:"file,omitempty"`
	Line        int      `json:"line,omitempty"`
	EndLine     int      `json:"end_line,omitempty"`
	Suggestion  string   `json:"suggestion,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
}

// ReviewInput is the immutable context supplied to every review agent.
type ReviewInput struct {
	Repository string `json:"repository,omitempty"`
	PullNumber int    `json:"pull_number,omitempty"`
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	Diff       string `json:"diff"`
}

// Review is the result of the complete review.
type Review struct {
	Findings []Finding `json:"findings"`
	Summary  string    `json:"summary,omitempty"`
	Approved bool      `json:"approved"`
}

// Agent is implemented by every source of review findings.
type Agent interface {
	Name() string
	Review(context.Context, ReviewInput) ([]Finding, error)
}

// ReviewAgent is retained as a descriptive alias for Agent.
type ReviewAgent = Agent
