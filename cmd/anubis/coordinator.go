package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

type Coordinator struct {
	Agents    []*Agent
	pr        *ReviewRequest
	LlmClient ChatCompleter
	result    *ReviewResult
	// MaxConcurrency caps how many specialists run at once. Zero means the
	// coordinator picks the safe default (sequential); see concurrency().
	MaxConcurrency int
	// failures records specialist agents that did not return a finding during
	// the last review, so the published comment can disclose partial coverage.
	failures []AgentFailure
}

// AgentFailure records a specialist agent that could not complete its review.
type AgentFailure struct {
	Name string
	Err  error
}

func NewCoordinator(agents []*Agent, repoName string, llmClient ChatCompleter) *Coordinator {
	c := Coordinator{
		Agents: agents,
		result: &ReviewResult{},
		pr: &ReviewRequest{
			RepoName: repoName,
		},
		LlmClient: llmClient,
	}
	return &c
}

func (c *Coordinator) FindingsText() string {
	if c == nil || c.result == nil {
		return ""
	}
	return c.result.ReviewFindingText
}

// Failures reports the specialist agents that failed during the last review. A
// non-empty result means the published review covers fewer perspectives than
// the full agent set and should say so.
func (c *Coordinator) Failures() []AgentFailure {
	if c == nil {
		return nil
	}
	return c.failures
}

func (c *Coordinator) SetPRdetails(pr *PullRequest, diff string) {
	if c == nil || pr == nil {
		return
	}
	c.pr.PullRequestTitle = pr.Title
	c.pr.PullRequestDescription = pr.Description
	c.pr.PullReqNumber = pr.Number
	c.pr.Diff = truncateDiff(diff)
}

// truncateDiff caps the diff sent to the model and records whether it was cut,
// so the published review can disclose that only part of the change was seen.
func truncateDiff(diff string) string {
	if len(diff) <= maxDiffBytes {
		return diff
	}
	truncated := diff[:maxDiffBytes]
	// Prefer a line boundary so the model does not see a mangled hunk.
	if i := strings.LastIndexByte(truncated, '\n'); i > 0 {
		truncated = truncated[:i]
	}
	return truncated + fmt.Sprintf(
		"\n\n... diff truncated: %d of %d bytes were shown to the model ...\n",
		len(truncated), len(diff),
	)
}

// DiffTruncated reports whether the diff exceeded the reviewable size limit.
func (c *Coordinator) DiffTruncated() bool {
	return c != nil && c.pr != nil &&
		strings.Contains(c.pr.Diff, "diff truncated")
}

// errNoFindings marks an agent that completed without producing any output.
var errNoFindings = errors.New("the model returned an empty review")

// maxConcurrentAgents bounds how many specialist reviewers run at once, so a
// large agent set cannot fan out into an unbounded number of in-flight
// provider requests. It is the ceiling used when the caller does not set
// MaxConcurrency explicitly; see defaultMaxConcurrency for why the shipped
// default is 1.
const maxConcurrentAgents = 4

// Review runs every specialist agent against the pull request, then asks the
// coordinator model to validate, deduplicate and synthesize their findings.
//
// Agents run concurrently but the findings are folded into the synthesis prompt
// in configured agent order, which keeps output stable across runs. A failing
// agent does not abort the review: as long as at least one agent produced a
// finding the synthesis still runs, and the failures are reported separately so
// the published review can disclose that it is incomplete.
func (c *Coordinator) Review(ctx context.Context) error {
	if c == nil {
		return errors.New("coordinator is invalid")
	}
	slog.Debug("coordinator started", "agents", len(c.Agents))
	defer slog.Debug("coordinator done")

	if len(c.Agents) == 0 {
		err := errors.New("no review agents configured")
		slog.Error("review failed", "error", err)
		return err
	}

	findings, failures := c.runAgents(ctx)
	c.failures = failures

	ok := 0
	for _, f := range findings {
		if f != "" {
			ok++
		}
	}
	if ok == 0 {
		err := allAgentsFailedError(c.Agents, failures)
		slog.Error("review failed", "error", err)
		return err
	}
	for _, f := range failures {
		slog.Warn("review agent failed, continuing with the remaining agents", "agent", f.Name, "error", f.Err)
	}

	var prompt strings.Builder
	for i, a := range c.Agents {
		addAgentFinding(&prompt, a.Name, findings[i])
	}

	finalPrompt := fmt.Sprintf(
		reviewPrompt,
		c.pr.RepoName,
		c.pr.PullReqNumber,
		c.pr.PullRequestTitle,
		c.pr.PullRequestDescription,
		c.pr.Diff,
		prompt.String(),
	)
	slog.Debug("synthesis prompt prepared", "bytes", len(finalPrompt))

	r := NewCompletionRequest(c.LlmClient.ModelName(), masterPrompt, finalPrompt)
	if r == nil {
		err := errors.New("unable to create completion request: model is not set")
		slog.Error("review failed", "error", err)
		return err
	}
	resp, err := c.LlmClient.Complete(ctx, r)
	if err != nil {
		err = fmt.Errorf("llm completion failed: %w", err)
		slog.Error("review failed", "error", err)
		return err
	}
	if resp == nil {
		err := errors.New("llm returned nil completion response")
		slog.Error("review failed", "error", err)
		return err
	}
	if resp.Completed() {
		c.result.ReviewStatus = StatusReviewCompleted
	} else {
		c.result.ReviewStatus = StatusReviewInComplete
		slog.Warn("synthesis truncated by the provider", "finish_reason", resp.FinishReason)
	}
	c.result.ReviewFindingText = resp.Content
	return nil
}

// allAgentsFailedError builds the error returned when no agent produced a
// finding. It has to tolerate an empty failure list: a provider that answers
// with an empty completion produces no finding and no error, and that must
// still read as a failed review rather than a clean one.
func allAgentsFailedError(agents []*Agent, failures []AgentFailure) error {
	names := make([]string, 0, len(failures))
	for _, f := range failures {
		names = append(names, f.Name)
	}
	if len(names) == 0 {
		return fmt.Errorf("all %d review agents returned no findings", len(agents))
	}
	cause := failures[0].Err
	if cause == nil {
		cause = errors.New("no findings returned")
	}
	return fmt.Errorf("all %d review agents failed (%s): %w", len(agents), strings.Join(names, ", "), cause)
}

// concurrency is the effective cap on simultaneous specialist reviews, never
// less than 1 and never more than the agent count.
func (c *Coordinator) concurrency() int {
	n := c.MaxConcurrency
	if n == 0 {
		n = defaultMaxConcurrency
	}
	return min(n, len(c.Agents), maxConcurrentAgents)
}

// runAgents executes the agents, at most MaxConcurrency at a time, and returns
// the findings in agent order along with the errors that occurred. A single
// error never cancels the remaining agents; the caller decides whether the
// partial result is usable. Findings are collected by index rather than appended
// so the order stays stable no matter what order the goroutines finish in.
func (c *Coordinator) runAgents(ctx context.Context) (findings []string, failures []AgentFailure) {
	findings = make([]string, len(c.Agents))
	errs := make([]AgentFailure, len(c.Agents))

	limit := c.concurrency()
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup

	for i, a := range c.Agents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errs[i] = AgentFailure{Name: a.name(), Err: ctx.Err()}
				return
			}
			if err := a.Review(ctx, c.pr); err != nil {
				errs[i] = AgentFailure{Name: a.name(), Err: err}
				return
			}
			if a.Finding == "" {
				// A provider that returns an empty completion produced no
				// evidence, which is not the same as an agent that reviewed the
				// change and found nothing. Counting it as a failure keeps it out
				// of the "no findings" path.
				errs[i] = AgentFailure{Name: a.name(), Err: errNoFindings}
				return
			}
			findings[i] = a.Finding
		}()
	}
	wg.Wait()

	for _, f := range errs {
		if f.Err != nil {
			failures = append(failures, f)
		}
	}
	return findings, failures
}

func addAgentFinding(b *strings.Builder, agentName, finding string) {
	if finding == "" {
		return
	}
	if agentName != "" {
		b.WriteString("Sub-agent reviewer name: ")
		b.WriteString(agentName)
		b.WriteString("\n")
	}
	b.WriteString("<reviewer-finding>")
	b.WriteString("\n")
	b.WriteString(finding)
	b.WriteString("\n")
	b.WriteString("</reviewer-finding>")
	b.WriteString("\n")
}
