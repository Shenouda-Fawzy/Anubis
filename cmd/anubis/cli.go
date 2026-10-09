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
		// When true, load review agents from .anubis-agents on the default branch
		loadAgents bool
	)
	flag.StringVar(&logLevel, "log-level", envOr("ANUBIS_LOG_LEVEL", "info"), "log level: debug, info, warn, error")
	flag.StringVar(&repo, "repo", os.Getenv("GITHUB_REPOSITORY"), "repository in 'owner/name' form")
	flag.Var(optionalInt{&pullReqNum}, "pr", "pull request number")
	flag.StringVar(&model, "model", envOr("ANUBIS_LLM_MODEL", defaultModel), "chat model")
	flag.StringVar(&baseURL, "llm-base-url", envOr("ANUBIS_LLM_BASE_URL", defaultLLMBaseURL), "OpenAI-compatible API base URL")
	flag.StringVar(&githubBaseURL, "github-base-url", envOr("GITHUB_API_URL", defaultGitHubBaseURL), "GitHub API base URL")
	flag.StringVar(&githubToken, "github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token")
	flag.BoolVar(&publishComment, "publish", false, "publish the review as a PR comment")
	flag.BoolVar(&loadAgents, "agents", envBool("ANUBIS_AGENTS", false), "load review agents from .anubis-agents on the default branch")

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

	agents := defaultAgents(llmClient)
	customAgents := false
	masterOverride := ""
	if loadAgents {
		set, err := loadCustomAgentGroup(ctx, ghb, owner, repoName, llmClient.Model)
		if err != nil {
			slog.Warn("could not load .anubis-agents; using the built-in agents", "error", err)
		} else {
			agents, customAgents = selectAgents(set.Specialists, llmClient)
			masterOverride = set.MasterPrompt
			slog.Debug("custom agent set resolved",
				"custom_specialists", len(set.Specialists), "builtins_replaced", customAgents,
				"custom_master", masterOverride != "", "ref", set.Ref)
		}
	}

	c := NewCoordinator(agents, repoName, llmClient)
	c.MaxConcurrency = maxConcurrency()
	if customAgents {
		c.AgentSource = agentsDir
	}
	c.MasterPrompt = masterOverride
	c.IsCustomMaster = masterOverride != ""

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
	if c.AgentSource != "" {
		fmt.Fprintf(&b, "\n\n---\n_User defined agents were loaded from `%s/` instead of the built-ins._", c.AgentSource)
	}
	if c.IsCustomMaster {
		fmt.Fprintf(&b, "\n\n---\n_User Defined Coordinator agent loaded from: `%s/%s`._", agentsDir, masterAgentFile)
	}
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

// llmAPIKeyEnvVar is the model-provider credential Anubis reads, and the only
// one. It is prefixed ANUBIS_ and deliberately names no provider: Anubis speaks
// one protocol to all of them, so the credential is whoever serves the model's
// key rather than a product Anubis has an opinion about. Provider-named
// variables such as OPENAI_API_KEY are not read, and must not be reintroduced —
// TestLLMAPIKeyIgnoresProviderNamedVariables exists to keep it that way.
//
//nolint:gosec // G101: this is an environment variable *name*, not a credential.
const llmAPIKeyEnvVar = "ANUBIS_LLM_API_KEY"

// llmAPIKey returns the configured provider credential, trimmed so a stray
// newline from a secret store cannot produce a confusing 401.
func llmAPIKey() string {
	return strings.TrimSpace(os.Getenv(llmAPIKeyEnvVar))
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
        chat model (env ANUBIS_LLM_MODEL, default "` + defaultModel + `")
  -llm-base-url string
        OpenAI-compatible API base URL (env ANUBIS_LLM_BASE_URL, default
        "` + defaultLLMBaseURL + `")
  -github-base-url string
        GitHub API base URL (env GITHUB_API_URL, default "` + defaultGitHubBaseURL + `")
  -github-token string
        GitHub token (env GITHUB_TOKEN)
  -publish
        publish the review as a PR comment
  -agents
        load review agents from .anubis-agents on the default branch
        (env ANUBIS_AGENTS)
  -log-level string
        log level: debug, info, warn, error (default "info")
  -h    show this help

Environment:
  GITHUB_TOKEN
        token used to read the pull request and post the review comment
  ANUBIS_LLM_API_KEY
        API key for the model endpoint. Any OpenAI-compatible provider works;
        see docs/providers.md for the base URL of each supported one.
  ANUBIS_LLM_BASE_URL, ANUBIS_LLM_MODEL
        base URL and model of that endpoint
  ANUBIS_MAX_CONCURRENCY
        how many specialist reviewers may run at once (default ` + strconv.Itoa(defaultMaxConcurrency) + `, which runs them one after another). Raise it to trade rate-limit headroom for wall-clock time.
  ANUBIS_AGENTS
        when true, load review agents from ` + agentsDir + `/ on the default
        branch. A directory with at least one valid *.md replaces the built-in
        specialists; ` + masterAgentFile + ` there overrides the coordinator.

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
// their findings. Each agent's system prompt is the embedded specialist prompt
// authored under cmd/anubis/specialist, so the review guidance lives as
// reviewable Markdown rather than Go string literals.
func defaultAgents(llmClient *OpenAIClient) []*Agent {
	specs := []struct {
		name         string
		description  string
		systemPrompt string
	}{
		{"security", "Look for authentication, authorization, injection, data exposure, and secret-handling issues.", securityAgentPrompt},
		{"correctness", "Look for concrete bugs, incorrect edge cases, and behavioral regressions introduced by this change.", correctnessAgentPrompt},
		{"performance", "Look for avoidable latency, excessive resource use, inefficient algorithms, and scalability problems.", performanceAgentPrompt},
		{"maintainability", "Look for maintainability, readability, testing, error-handling, and established best-practice problems.", maintainabilityAgentPrompt},
	}
	agents := make([]*Agent, 0, len(specs))
	for _, s := range specs {
		agents = append(agents, &Agent{
			AgentCard: &AgentCard{
				Name:         s.name,
				Description:  s.description,
				Model:        llmClient.Model,
				SystemPrompt: s.systemPrompt,
			},
			LlmClient: llmClient,
		})
	}
	return agents
}
