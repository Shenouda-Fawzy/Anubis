package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

const (
	agentsDir       = ".anubis-agents"
	masterAgentFile = "master-agent.md"
)

const (
	maxAgentFiles     = 8
	maxAgentFileBytes = 64 * 1024
)

// defaultAgentFocus is the task focus used when a custom agent omits its
// front-matter description. It is intentionally generic: the system prompt
// carries the real instructions.
const defaultAgentFocus = "Review the diff according to your system instructions and return the findings."

type agentGroup struct {
	Specialists  []*AgentCard
	MasterPrompt string
	Ref          string
}

// Load custom agent group from the repo, use GH api to directly locate it
// the directory (.anubis-agents) should be in the root anyway
func loadCustomAgentGroup(ctx context.Context, gh *Client, owner, repo, defaultModel string) (*agentGroup, error) {
	if gh == nil {
		return nil, errors.New("GH client not init")
	}
	branchName, err := gh.GetDefaultBranch(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("resolve default branch: %w", err)
	}
	return loadAgentGroup(ctx, gh, owner, repo, branchName, defaultModel)
}

// load the custom agents via GH directory listing APIs
func loadAgentGroup(ctx context.Context, gh *Client, owner, repo, branchName, defaultModel string) (*agentGroup, error) {
	if gh == nil {
		return nil, errors.New("nil github client")
	}
	entries, err := gh.ListDirectory(ctx, owner, repo, agentsDir, branchName)
	if err != nil {
		if isGitHubNotFound(err) {
			slog.Debug("no custom agents directory", "path", agentsDir, "ref", branchName)
			return nil, nil
		}
		return nil, fmt.Errorf("list %s: %w", agentsDir, err)
	}

	// First locate the master agent (master-agent.md) then load the other
	// remaining agents
	master := ""
	for _, e := range entries {
		if e.Type != "file" || strings.HasSuffix(e.Name, ".md") == false {
			continue
		}
		if e.Name == masterAgentFile {
			prompt, err := readAgentBody(ctx, gh, owner, repo, e, branchName)
			if err != nil {
				slog.Warn("ignoring custom coordinator prompt", "file", e.Name, "error", err)
				continue
			}
			master = prompt
		}
	}

	subAgents := make([]directoryEntry, 0, len(entries))
	for _, e := range entries {
		if e.Type == "file" && strings.HasSuffix(e.Name, ".md") && e.Name != masterAgentFile {
			subAgents = append(subAgents, e)
		}
	}
	sort.Slice(subAgents, func(i, j int) bool { return subAgents[i].Name < subAgents[j].Name })

	if len(subAgents) > maxAgentFiles {
		slog.Warn("too many custom agent files, using the first ones",
			"limit", maxAgentFiles, "found", len(subAgents))
		subAgents = subAgents[:maxAgentFiles]
	}

	groupAgents := agentGroup{MasterPrompt: master, Ref: branchName}
	for _, e := range subAgents {
		card, err := readAgentCard(ctx, gh, owner, repo, e, branchName, defaultModel)
		if err != nil {
			slog.Warn("ignoring custom agent", "file", e.Name, "error", err)
			continue
		}
		groupAgents.Specialists = append(groupAgents.Specialists, card)
	}
	return &groupAgents, nil
}

// readAgentBody fetches one file and returns its prompt body with any
// front-matter stripped. It is the coordinator override path, which cares only
// about the body.
func readAgentBody(ctx context.Context, gh *Client, owner, repo string, e directoryEntry, ref string) (string, error) {
	card, err := readAgentCard(ctx, gh, owner, repo, e, ref, "")
	if err != nil {
		return "", err
	}
	return card.SystemPrompt, nil
}

// readAgentCard fetches and parses one custom specialist file, enforcing the
// size cap before spending a request.
func readAgentCard(ctx context.Context, gh *Client, owner, repo string, e directoryEntry, ref, defaultModel string) (*AgentCard, error) {
	if e.Size > maxAgentFileBytes {
		return nil, fmt.Errorf("%s is %d bytes, over the %d-byte limit", e.Path, e.Size, maxAgentFileBytes)
	}
	raw, err := gh.GetFileContent(ctx, owner, repo, e.Path, ref)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxAgentFileBytes {
		return nil, fmt.Errorf("%s is %d bytes, over the %d-byte limit", e.Path, len(raw), maxAgentFileBytes)
	}
	card, err := parseAgentCard(e.Name, []byte(raw))
	if err != nil {
		return nil, err
	}
	if card.Model == "" {
		card.Model = defaultModel
	}
	return card, nil
}

func parseAgentCard(fileName string, raw []byte) (*AgentCard, error) {
	text := strings.TrimPrefix(string(raw), "\ufeff")
	card := &AgentCard{}
	body := text
	if front, rest, ok := splitFrontMatter(text); ok {
		card.Name, card.Description, card.Model = parseFrontMatter(front)
		body = rest
	}
	card.SystemPrompt = strings.TrimSpace(body)
	if card.SystemPrompt == "" {
		return nil, fmt.Errorf("agent file %q has an empty prompt body", fileName)
	}
	if card.Name == "" {
		card.Name = strings.TrimSuffix(fileName, ".md")
	}
	card.Name = strings.TrimSpace(card.Name)
	if card.Name == "" {
		return nil, fmt.Errorf("agent file %q has no name", fileName)
	}
	if card.Description == "" {
		card.Description = defaultAgentFocus
	}
	return card, nil
}

func splitFrontMatter(s string) (front, rest string, ok bool) {
	lines := strings.Split(s, "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		return "", s, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), true
		}
	}
	return "", s, false
}

func parseFrontMatter(front string) (name, description, model string) {
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = trimQuotes(strings.TrimSpace(value))
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			name = value
		case "description":
			description = value
		case "model":
			model = value
		}
	}
	return name, description, model
}

// trimQuotes removes one layer of matching single or double quotes from a
// front-matter value, so `name: "security"` and `name: security` agree.
func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// use default agents if no custom agents provided
func selectAgents(custom []*AgentCard, llmClient *OpenAIClient) ([]*Agent, bool) {
	if len(custom) == 0 {
		return defaultAgents(llmClient), false
	}
	agents := make([]*Agent, 0, len(custom))
	for _, card := range custom {
		if card.Model == "" {
			card.Model = llmClient.Model
		}
		agents = append(agents, &Agent{AgentCard: card, LlmClient: llmClient})
	}
	return agents, true
}
