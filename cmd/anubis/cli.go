package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// This will be main entry

// optionalInt is an int flag that accepts an empty value. An input that is unset
// in a workflow expands to the empty string, which strconv rejects with a raw
// parse error; treating it as unset instead lets the required-flag check report
// it in the program's own words.
type optionalInt struct{ v *int }

func (o optionalInt) String() string {
	if o.v == nil {
		return ""
	}
	return strconv.Itoa(*o.v)
}

func (o optionalInt) Set(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		*o.v = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*o.v = n
	return nil
}

func Review() {
	var (
		repo          string
		pullReqNum    int
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
	flag.Var(optionalInt{&pullReqNum}, "pr", "pull request number")
	flag.StringVar(&model, "model", envOr("ANUBIS_MODEL", defaultModel), "chat model")
	flag.StringVar(&baseURL, "llm-base-url", envOr("ANUBIS_LLM_BASE_URL", defaultLLMBaseURL), "OpenAI-compatible API base URL")
	flag.StringVar(&githubBaseURL, "github-base-url", envOr("GITHUB_API_URL", defaultGitHubBaseURL), "GitHub API base URL")
	flag.StringVar(&githubToken, "github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token")
	flag.BoolVar(&publishComment, "publish", false, "publish the review as a PR comment")

	flag.Usage = func() {
		_, _ = fmt.Fprint(flag.CommandLine.Output(), longHelp())
	}
	flag.Parse()

	setupLogger(logLevel)
	slog.Info("Anubis", "version", version)

	if repo == "" || pullReqNum <= 0 {
		fatal("both -repo and -pr are required")
	}
	repo = strings.TrimSpace(repo)
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		fatal("-repo must be owner/name")
	}
	ghb, err := NewGithubClient(githubToken, githubBaseURL)
	if err != nil {
		fatal(err.Error())
		return
	}
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

	llmClient := NewOpenAIClient(llmAPIKey(), baseURL, model)
	slog.Debug("LLM client configured", "base_url", llmClient.BaseURL, "model", llmClient.Model)

	c := NewCoordinator(defaultAgents(llmClient), repoName, llmClient)
	c.MaxConcurrency = maxConcurrency()

	c.SetPRdetails(&pr, diff)
	slog.Debug("coordinator created", "agent_count", len(c.Agents), "max_concurrency", c.MaxConcurrency)

	reviewErr := c.Review(ctx)
	comment := noFindingsComment
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
		comment += caveats(c)
		url, err := ghb.CreateComment(ctx, owner, repoName, pullReqNum, comment)
		if err != nil {
			fatal(err.Error())
			return
		}
		slog.Info("comment posted", "url", url)
	}
	if reviewErr != nil {
		fatal(reviewErr.Error())
	}
}

// caveats appends the disclosures a reader needs in order to judge how much
// coverage the review actually had. A review that silently dropped reviewers or
// silently truncated a huge diff reads as a clean bill of health, which it is not.
func caveats(c *Coordinator) string {
	var b strings.Builder
	if c.DiffTruncated() {
		b.WriteString("\n\n---\n_Diff too large to review in full; findings cover a truncated diff._")
	}
	if f := c.Failures(); len(f) > 0 {
		names := make([]string, 0, len(f))
		for _, x := range f {
			names = append(names, x.Name)
		}
		fmt.Fprintf(&b, "\n\n---\n_%d of %d review agents failed (%s), so findings cover the remaining agents only._",
			len(f), len(c.Agents), strings.Join(names, ", "))
	}
	return b.String()
}

// llmAPIKey resolves the model provider credential. Providers disagree on the
// variable name, so the documented ones are tried in order and the first
// non-empty value wins: OPENCODE_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, then
// AI_API_KEY.
func llmAPIKey() string {
	for _, name := range []string{"OPENCODE_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "AI_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
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

func longHelp() string {
	return `Anubis - AI pull-request review agent

Anubis runs several specialist review agents against a GitHub pull-request diff
in parallel, then has a coordinator model validate and deduplicate their
findings into a single high-signal review. Optionally publishes the result as a
comment on the pull request.

Usage:
  anubis -repo owner/name -pr NUMBER [flags]

Flags:
  -repo string
        repository in 'owner/name' form (env GITHUB_REPOSITORY)
  -pr int
        pull request number (required)
  -model string
        chat model (env ANUBIS_MODEL, default "` + defaultModel + `")
  -llm-base-url string
        OpenAI-compatible API base URL (env ANUBIS_LLM_BASE_URL, default
        "` + defaultLLMBaseURL + `")
  -github-base-url string
        GitHub API base URL (env GITHUB_API_URL, default "` + defaultGitHubBaseURL + `")
  -github-token string
        GitHub token (env GITHUB_TOKEN)
  -publish
        publish the review as a PR comment
  -log-level string
        log level: debug, info, warn, error (default "info")
  -h    show this help

Environment:
  GITHUB_TOKEN
        token used to read the pull request and post the review comment
  OPENCODE_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY or AI_API_KEY
        API key for the model provider, tried in that order
  ANUBIS_MAX_CONCURRENCY
        how many specialist reviewers may run at once (default ` + strconv.Itoa(defaultMaxConcurrency) + `, which runs them one after another). Raise it to trade rate-limit headroom for wall-clock time.

Examples:
  anubis -repo owner/name -pr 42
  anubis -repo owner/name -pr 42 -publish
  anubis -repo owner/name -pr 42 -log-level debug
  anubis -repo owner/name -pr 42 \
    -llm-base-url http://localhost:11434/v1 -model qwen3-coder
`
}

// defaultAgents returns the built-in specialist reviewers. Every agent reviews
// the whole diff through its own lens; the coordinator then validates and merges
// their findings.
func defaultAgents(llmClient *OpenAIClient) []*Agent {
	descriptions := []struct{ name, description string }{
		{"security", "Look for authentication, authorization, injection, data exposure, and secret-handling issues."},
		{"correctness", "Look for concrete bugs, incorrect edge cases, and behavioral regressions introduced by this change."},
		{"performance", "Look for avoidable latency, excessive resource use, inefficient algorithms, and scalability problems."},
		{"maintainability", "Look for maintainability, readability, testing, error-handling, and established best-practice problems."},
	}
	agents := make([]*Agent, 0, len(descriptions))
	for _, d := range descriptions {
		agents = append(agents, &Agent{
			AgentCard: &AgentCard{
				Name:        d.name,
				Description: d.description,
				Model:       llmClient.Model,
			},
			LlmClient: llmClient,
		})
	}
	return agents
}
