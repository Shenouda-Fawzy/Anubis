// Package domain contains the types shared by Anubis adapters and services.
package domain

import (
	"context"
	"strconv"
	"strings"
)

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
	ID          string     `json:"id,omitempty"`
	Agent       string     `json:"agent,omitempty"`
	Severity    Severity   `json:"severity"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	File        string     `json:"file,omitempty"`
	Line        int        `json:"line,omitempty"`
	EndLine     int        `json:"end_line,omitempty"`
	Suggestion  string     `json:"suggestion,omitempty"`
	Confidence  Confidence `json:"confidence,omitempty"`
}

// Confidence is a 0-1 confidence score that tolerates the various shapes LLMs
// return: a JSON number (0.9), a numeric string ("0.9"), a percentage string
// ("90%"), or a free-form word ("high") resolved to a best-effort score.
type Confidence float64

func (c *Confidence) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(strings.Trim(string(data), `"`))
	switch s {
	case "", "null":
		*c = 0
		return nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		*c = Confidence(f)
		return nil
	}
	if strings.HasSuffix(s, "%") && len(s) > 1 {
		if p, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64); err == nil {
			*c = Confidence(p / 100)
			return nil
		}
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "very low":
		*c = 0.1
	case "low":
		*c = 0.3
	case "medium", "moderate":
		*c = 0.6
	case "high":
		*c = 0.8
	case "very high":
		*c = 0.9
	case "certain", "definite":
		*c = 1.0
	default:
		*c = 0.5
	}
	return nil
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
