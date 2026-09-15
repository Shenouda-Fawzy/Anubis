package main

import (
	"context"
	"fmt"
	"log"
	"strings"
)

type Coordinator struct {
	Agents    []*Agent
	pr        *ReviewRequest
	LlmClient ChatCompleter
	result    *ReviewResult
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

func (c *Coordinator) SetPRdetails(pr *PullRequest, diff string) {
	if c == nil || pr == nil {
		return
	}
	c.pr.PullRequestTitle = pr.Title
	c.pr.PullRequestDescription = pr.Description
	c.pr.PullReqNumber = pr.Number
	c.pr.Diff = diff
}

// Get review from all sub-agents
// Combine all of it into single prompt and send it to LLM
// Ask LLM to deduplicate and synthesize
// Get result as JSON object

func (c *Coordinator) Review(ctx context.Context) {
	log.Println("Coordinator started")
	defer log.Println("Coordinator done")
	if c == nil {
		log.Println("coordinator is invalid")
		return
	}
	// It should never happen as there will always be an agent either
	// user provided or default agent
	if len(c.Agents) == 0 {
		log.Println("no agents, please ensure there is a least one agent")
		return
	}
	prompt := strings.Builder{}
	// Now each agent will do its own review, and will keep it in its memory
	for _, a := range c.Agents {
		log.Printf("Agent Card %#v\n", a.AgentCard)
		err := a.Review(ctx, c.pr)
		// defer a.Done()
		if err != nil {
			log.Println(err)
			return
		}
		addAgentFinding(&prompt, a.Name, a.Finding)
	}
	finalPrompt := fmt.Sprintf(
		reviewPrompt,
		c.pr.RepoName,
		c.pr.PullRequestTitle,
		c.pr.PullRequestDescription,
		c.pr.Diff,
		prompt.String(),
	)

	fmt.Println("--- final prompt ---")
	fmt.Println(finalPrompt)
	fmt.Println("--- final prompt done ---")

	r := NewCompletionRequest(c.LlmClient.ModelName(), masterPrompt, finalPrompt)
	log.Printf("Completion Request obj %#v\n", r)
	resp, err := c.LlmClient.Complete(ctx, r)
	if err != nil {
		log.Println(err)
		return
	}
	log.Printf("Coordinator Result = %#v\n", resp)
	if resp.Completed() == false {
		c.result.ReviewStatus = StatusReviewInComplete
	} else {
		c.result.ReviewStatus = StatusReviewCompleted
	}
	c.result.ReviewFindingText = resp.Content
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

var reviewPrompt = `# Pull Request

<pr>
  <repository>%s</repository>
  <pull_request>%d</pull_request>
  <title>%s</title>

  <description>
   %s
  </description>
</pr>

# Diff

%s

# Specialist Findings

The following findings were independently produced by specialized review agents.

Treat them as **candidate findings**, not established facts.

<reviewer_findings>

  %s

</reviewer_findings>

# Task

Perform the final coordination pass.

For every candidate finding:

1. Determine whether it is actually valid.
2. Verify it against the available code and diff.
3. Reject speculative or unsupported findings.
4. Deduplicate overlapping findings.
5. Merge findings describing the same root cause.
6. Correct the category when necessary.
7. Correct the severity when necessary.
8. Keep only actionable findings that are relevant to this pull request.

Then return the final findings using the required output schema.
`
