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
	llmClient := llm.NewOpenAIClient(envOr("AI_API_KEY", ""), baseURL, model)

	// Load user-provided agents and if not fallback to default agents
	var markdown []*agents.SubAgent
	var err error
	if agentDir != "" {
		markdown, err = agents.LoadDir(agentDir, llmClient)
		if err != nil {
			fatal(err.Error())
		}
	}
	if len(markdown) == 0 {
		markdown = agents.BuiltinAgents(llmClient)
	}
	log.Printf("Owner Name=%s, Repo=%s\n", parts[0], parts[1])
	api := github.NewClient(githubToken, githubBaseURL)
	ctx := context.Background()
	pr, err := api.GetPullRequest(ctx, parts[0], parts[1], pullReqNum)
	if err != nil {
		fatal(err.Error())
	}
	diff, err := api.GetDiff(ctx, parts[0], parts[1], pullReqNum)
	if err != nil {
		fatal(err.Error())
	}
	specialAgents := make([]domain.Agent, len(markdown))
	for i := range markdown {
		specialAgents[i] = markdown[i]
	}
	orchestra := orchestrator.New(specialAgents, llmClient)
	review, err := orchestra.Review(ctx, domain.ReviewInput{Repository: repo, PullNumber: pullReqNum, PrTitle: pr.Title, PrDescription: pr.Description, PrDiff: diff})
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
		return
	}
	encoded, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		fatal(fmt.Sprintf("encode review: %v", err))
	}
	fmt.Println(string(encoded))
	if publishComment {
		if err := api.PublishReview(ctx, parts[0], parts[1], pullReqNum, review); err != nil {
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
