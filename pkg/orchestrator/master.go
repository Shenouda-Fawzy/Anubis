// Package orchestrator coordinates independent agents and synthesizes a review.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Shenouda-Fawzy/Anubis/pkg/domain"
	"github.com/Shenouda-Fawzy/Anubis/pkg/llm"
)

type Master struct {
	Agents         []domain.Agent
	Client         llm.Client
	MaxConcurrency int
}

func New(agents []domain.Agent, client llm.Client) *Master {
	return &Master{Agents: agents, Client: client, MaxConcurrency: 4}
}

type agentResult struct {
	index    int
	findings []domain.Finding
	err      error
}

// Review runs all agents concurrently, preserving agent order in the result.
func (m *Master) Review(ctx context.Context, input domain.ReviewInput) (domain.Review, error) {
	if m == nil {
		return domain.Review{}, fmt.Errorf("orchestrator: nil master")
	}
	limit := m.MaxConcurrency
	if limit <= 0 {
		limit = 1
	}
	if limit > len(m.Agents) {
		limit = len(m.Agents)
	}
	results := make(chan agentResult, len(m.Agents))
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, agent := range m.Agents {
		if agent == nil {
			continue
		}
		wg.Add(1)
		go func(i int, agent domain.Agent) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results <- agentResult{index: i, err: ctx.Err()}
				return
			}
			findings, err := agent.Review(ctx, input)
			<-sem
			results <- agentResult{index: i, findings: findings, err: err}
		}(i, agent)
	}
	wg.Wait()
	close(results)
	ordered := make([]agentResult, 0, len(results))
	var errs []string
	for result := range results {
		ordered = append(ordered, result)
		if result.err != nil {
			errs = append(errs, result.err.Error())
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].index < ordered[j].index })
	review := domain.Review{Findings: deduplicate(ordered)}
	review.Approved = !hasBlocking(review.Findings)
	if m.Client != nil {
		summary, err := m.synthesize(ctx, input, review.Findings)
		if err != nil {
			errs = append(errs, err.Error())
		} else {
			review.Summary = summary
		}
	}
	if review.Summary == "" {
		review.Summary = fallbackSummary(review.Findings)
	}
	if len(errs) > 0 {
		return review, fmt.Errorf("orchestrator: %s", strings.Join(errs, "; "))
	}
	return review, nil
}

func deduplicate(results []agentResult) []domain.Finding {
	seen := make(map[string]bool)
	var output []domain.Finding
	for _, result := range results {
		for _, finding := range result.findings {
			key := finding.ID
			if key == "" {
				key = fmt.Sprintf("%s|%d|%d|%s", finding.File, finding.Line, finding.EndLine, finding.Title)
			}
			key = strings.ToLower(strings.TrimSpace(key))
			if seen[key] {
				continue
			}

			seen[key] = true
			output = append(output, finding)
		}
	}
	return output
}

// DeduplicateFindings removes repeated observations while preserving order.
func DeduplicateFindings(findings []domain.Finding) []domain.Finding {
	return deduplicate([]agentResult{{findings: findings}})
}

func hasBlocking(findings []domain.Finding) bool {
	for _, f := range findings {
		if f.Severity == domain.SeverityCritical || f.Severity == domain.SeverityHigh {
			return true
		}
	}
	return false
}

func fallbackSummary(findings []domain.Finding) string {
	if len(findings) == 0 {
		return "No actionable issues found."
	}
	return fmt.Sprintf("Found %d actionable issue(s) requiring review.", len(findings))
}

func (m *Master) synthesize(ctx context.Context, input domain.ReviewInput, findings []domain.Finding) (string, error) {
	data, err := json.Marshal(findings)
	if err != nil {
		return "", fmt.Errorf("synthesis: encode findings: %w", err)
	}
	response, err := m.Client.Complete(ctx, llm.CompletionRequest{Messages: []llm.Message{
		{Role: "system", Content: "You summarize code review findings concisely and accurately."},
		{Role: "user", Content: fmt.Sprintf("Summarize these findings for a pull request. Do not add new claims.\nContext: %s\nFindings JSON: %s", input.Title, data)},
	}, MaxTokens: 2048})
	if err != nil {
		return "", fmt.Errorf("synthesis: %w", err)
	}
	return strings.TrimSpace(response.Content), nil
}
