package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// This will be main entry

func Review() {
	var (
		repo          string
		pullReqNum    int
		agentDir      string
		model         string
		baseURL       string
		githubBaseURL string
		githubToken   string
		logLevel      string
		// When true, comment will be added to the PR
		publishComment bool
	)
	flag.StringVar(&logLevel, "log-level", envOr("ANUBIS_LOG_LEVEL", "info"), "log level: debug, info, warn, error")
	flag.StringVar(&repo, "repo", os.Getenv("GITHUB_REPOSITORY"), "repository in 'owner/name' form")
	flag.IntVar(&pullReqNum, "pr", 0, "pull request number")
	flag.StringVar(&agentDir, "agents", "", "directory containing Markdown agents")
	flag.StringVar(&model, "model", envOr("ANUBIS_MODEL", ""), "chat model")
	flag.StringVar(&baseURL, "llm-base-url", envOr("ANUBIS_LLM_BASE_URL", ""), "OpenAI-compatible API base URL (default is OpenCode Zen)")
	flag.StringVar(&githubBaseURL, "github-base-url", envOr("GITHUB_API_URL", ""), "GitHub API base URL")
	flag.StringVar(&githubToken, "github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token")
	flag.BoolVar(&publishComment, "publish", false, "publish the review as a PR comment")

	flag.Parse()

	setupLogger(logLevel)
	slog.Info("Anubis")

	if repo == "" || pullReqNum <= 0 {
		fatal("both -repo and -pr are required")
	}
	repo = strings.TrimSpace(repo)
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		fatal("-repo must be owner/name")
	}
	ghb := NewGithubClient(githubToken, githubBaseURL)
	ctx := context.Background()
	pr, err := ghb.GetPullRequest(ctx, parts[0], parts[1], pullReqNum)
	if err != nil {
		fatal(err.Error())
		return
	}
	slog.Debug("pull request loaded", "number", pr.Number, "title", pr.Title)
	owner := parts[0]
	repoName := parts[1]
	diff, err := ghb.GetDiff(ctx, owner, repoName, pullReqNum)
	if err != nil {
		fatal(err.Error())
		return
	}

	llmClient := NewOpenAIClient(envOr("AI_API_KEY", ""), baseURL, model)
	slog.Debug("LLM client configured", "base_url", llmClient.BaseURL, "model", llmClient.Model)

	// Create default agents
	// Then add support for user defined agents

	c := NewCoordinator(defaultAgents(llmClient), repoName, llmClient)

	c.SetPRdetails(&pr, diff)
	slog.Debug("coordinator created", "agent_count", len(c.Agents))

	reviewErr := c.Review(context.Background())
	comment := "No findings"
	if reviewErr != nil {
		slog.Error("review failed", "error", reviewErr)
		comment = failureComment(reviewErr)
	}
	if publishComment {
		slog.Info("publishing comment")
		if reviewErr == nil && c.FindingsText() != "" {
			slog.Debug("findings found")
			comment = c.FindingsText()
		}
		if err := ghb.CreateComment(ctx, owner, repoName, pullReqNum, comment); err != nil {
			fatal(err.Error())
			return
		}
	}
	if reviewErr != nil {
		fatal(reviewErr.Error())
	}
}

// failureComment builds the PR comment posted when the review couldn't
// complete (e.g. LLM outage/rate limit), so readers know it is not a clean
// review rather than silently reporting "No findings".
func failureComment(err error) string {
	return fmt.Sprintf(
		"⚠️ **Anubis could not complete the review**\n\n"+
			"The LLM review step failed, so no trustworthy findings were produced. "+
			"This is **not** a clean review — please inspect the PR manually and re-run "+
			"Anubis once the LLM service recovers.\n\nError: %v",
		err,
	)
}

/*
{Title: "security", Description: "Find security vulnerabilities.", Prompt: "Look for authentication, authorization, injection, data exposure, and secret-handling issues.", Severity: string(domain.SeverityHigh)},

	{Name: "performance", Description: "Find performance regressions.", Prompt: "Look for avoidable latency, excessive resource use, inefficient algorithms, and scalability problems.", Severity: string(domain.SeverityMedium)},
	{Name: "coding-standards", Description: "Find coding-standard and best-practice issues.", Prompt: "Look for maintainability, readability, testing, error-handling, and established best-practice problems.", Severity: string(domain.SeverityMedium)},
*/
func defaultAgents(llmClient *OpenAIClient) []*Agent {
	agents := []*Agent{
		{
			AgentCard: &AgentCard{
				Name: "performance", Description: "Look for avoidable latency, excessive resource use, inefficient algorithms, and scalability problems.",
				Model: llmClient.Model,
			},
			LlmClient: llmClient,
		},
		{
			AgentCard: &AgentCard{
				Name: "security", Description: "Look for authentication, authorization, injection, data exposure, and secret-handling issues.",
				Model: llmClient.Model,
			},
			LlmClient: llmClient,
		},
		{
			AgentCard: &AgentCard{
				Name: "coding-standards", Description: "Look for maintainability, readability, testing, error-handling, and established best-practice problems.",
				Model: llmClient.Model,
			},
			LlmClient: llmClient,
		},
	}
	return agents
}
