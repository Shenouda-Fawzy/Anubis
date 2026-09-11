package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/Shenouda-Fawzy/Anubis/pkg/llm"
)

// This will be main entry

func Review() {
	// Print file location as will as timestamp in UTC
	log.SetFlags(log.Llongfile | log.Ltime | log.Ldate | log.LUTC)
	var (
		repo          string
		pullReqNum    int
		agentDir      string
		model         string
		baseURL       string
		githubBaseURL string
		githubToken   string
		// When true, comment will be added to the PR
		publishComment bool
	)
	log.Print("Anubis")
	flag.StringVar(&repo, "repo", os.Getenv("GITHUB_REPOSITORY"), "repository in 'owner/name' form")
	flag.IntVar(&pullReqNum, "pr", 0, "pull request number")
	flag.StringVar(&agentDir, "agents", "", "directory containing Markdown agents")
	flag.StringVar(&model, "model", envOr("ANUBIS_MODEL", llm.DefaultModel), "chat model")
	flag.StringVar(&baseURL, "llm-base-url", envOr("ANUBIS_LLM_BASE_URL", llm.DefaultBaseURL), "OpenAI-compatible API base URL (default is OpenCode Zen)")
	flag.StringVar(&githubBaseURL, "github-base-url", envOr("GITHUB_API_URL", ""), "GitHub API base URL")
	flag.StringVar(&githubToken, "github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token")
	flag.BoolVar(&publishComment, "publish", false, "publish the review as a PR comment")

	flag.Parse()

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
	log.Printf("Pull Request = %#v\n", pr)
	owner := parts[0]
	repoName := parts[1]
	diff, err := ghb.GetDiff(ctx, owner, repoName, pullReqNum)
	if err != nil {
		fatal(err.Error())
		return
	}

	llmClient := NewOpenAIClient(envOr("AI_API_KEY", ""), baseURL, model)
	log.Printf("LLM client %#v\n", llmClient)

	// Create default agents
	// Then add support for user defined agents

	c := NewCoordinator(defaultAgents(llmClient), repoName, llmClient)

	c.SetPRdetails(&pr, diff)
	log.Printf("Coordinator %#v\n", c)

	c.Review(context.Background())
	comment := "No findings"
	if publishComment {
		log.Println("Publishing comment")
		if c.FindingsText() != "" {
			log.Println("Found findings")
			comment = c.FindingsText()
		}
		if err := ghb.CreateComment(ctx, owner, repoName, pullReqNum, comment); err != nil {
			fatal(err.Error())
			return
		}
	}
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
