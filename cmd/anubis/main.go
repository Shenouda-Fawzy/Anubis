package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Shenouda-Fawzy/Anubis/pkg/agents"
	"github.com/Shenouda-Fawzy/Anubis/pkg/domain"
	"github.com/Shenouda-Fawzy/Anubis/pkg/github"
	"github.com/Shenouda-Fawzy/Anubis/pkg/llm"
	"github.com/Shenouda-Fawzy/Anubis/pkg/orchestrator"
)

func main() {
	log.SetFlags(log.Llongfile | log.Ltime)
	repo := flag.String("repo", os.Getenv("GITHUB_REPOSITORY"), "repository in owner/name form")
	prNumber := flag.Int("pr", 0, "pull request number")
	agentDir := flag.String("agents", "", "directory containing Markdown agents")
	model := flag.String("model", envOr("ANUBIS_MODEL", llm.DefaultModel), "chat model")
	baseURL := flag.String("llm-base-url", envOr("ANUBIS_LLM_BASE_URL", llm.DefaultBaseURL), "OpenAI-compatible API base URL (default is OpenCode Zen)")
	githubBaseURL := flag.String("github-base-url", envOr("GITHUB_API_URL", ""), "GitHub API base URL")
	token := flag.String("github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token")
	publish := flag.Bool("publish", false, "publish the review as a PR comment")
	flag.Parse()
	if *repo == "" || *prNumber <= 0 {
		fatal("both -repo and -pr are required")
	}
	parts := strings.SplitN(*repo, "/", 2)
	if len(parts) != 2 {
		fatal("-repo must be owner/name")
	}
	llmClient := llm.NewOpenAIClient(envOr("OPENCODE_API_KEY", envOr("OPENAI_API_KEY", os.Getenv("GEMINI_API_KEY"))), *baseURL, *model)
	var markdown []*agents.MarkdownAgent
	var err error
	if *agentDir != "" {
		markdown, err = agents.LoadDir(*agentDir, llmClient)
		if err != nil {
			fatal(err.Error())
		}
	}
	if len(markdown) == 0 {
		// TODO:
		// 	- Built in agent should be reading from the agents/ directory
		markdown = agents.BuiltinAgents(llmClient)
	}
	log.Printf("Parts=%s, Repo=%s", parts[0], parts[1])
	api := github.NewClient(*token, *githubBaseURL)
	ctx := context.Background()
	pr, err := api.GetPullRequest(ctx, parts[0], parts[1], *prNumber)
	if err != nil {
		fatal(err.Error())
	}
	diff, err := api.GetDiff(ctx, parts[0], parts[1], *prNumber)
	if err != nil {
		fatal(err.Error())
	}
	domainAgents := make([]domain.Agent, len(markdown))
	for i := range markdown {
		domainAgents[i] = markdown[i]
	}
	orchestra := orchestrator.New(domainAgents, llmClient)
	review, err := orchestra.Review(ctx, domain.ReviewInput{Repository: *repo, PullNumber: *prNumber, Title: pr.Title, Body: pr.Body, Diff: diff})
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
	encoded, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		fatal(fmt.Sprintf("encode review: %v", err))
	}
	fmt.Println(string(encoded))
	if *publish {
		if err := api.PublishReview(ctx, parts[0], parts[1], *prNumber, review); err != nil {
			fatal(err.Error())
		}
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(2) }
