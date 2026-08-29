// Package agents loads Markdown-defined review agents.
package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Shenouda-Fawzy/Anubis/pkg/domain"
	"github.com/Shenouda-Fawzy/Anubis/pkg/llm"
	"gopkg.in/yaml.v3"
)

// Definition is the YAML front matter and Markdown prompt for an agent.
type Definition struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	System       string   `yaml:"system"`
	SystemPrompt string   `yaml:"system_prompt"`
	Prompt       string   `yaml:"prompt"`
	UserPrompt   string   `yaml:"user_prompt"`
	ReviewPrompt string   `yaml:"review_prompt"`
	Severity     string   `yaml:"severity"`
	Enabled      *bool    `yaml:"enabled"`
	Model        string   `yaml:"model"`
	MaxTokens    int      `yaml:"max_tokens"`
	Temperature  *float64 `yaml:"temperature"`
}

// MarkdownAgent turns a Definition into a domain.Agent.
type MarkdownAgent struct {
	Definition Definition
	Client     llm.Client
}

func (a *MarkdownAgent) Name() string { return a.Definition.Name }

func (a *MarkdownAgent) Review(ctx context.Context, input domain.ReviewInput) ([]domain.Finding, error) {
	if a == nil || a.Client == nil {
		return nil, errors.New("agent: an LLM client is required")
	}
	d := withDefaults(a.Definition)
	prompt := strings.TrimSpace(d.Prompt)
	if prompt == "" {
		prompt = "Review the pull request diff and report actionable issues."
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("agent %s: encode input: %w", d.Name, err)
	}
	req := llm.CompletionRequest{Model: d.Model, Messages: []llm.Message{
		{Role: "system", Content: d.System},
		{Role: "user", Content: prompt + "\n\nPull request context (JSON):\n" + string(payload) + "\n\nReturn only a JSON array of findings. Each finding must include severity, title, description, and optionally file, line, end_line, suggestion, confidence."},
	}, MaxTokens: d.MaxTokens, Temperature: d.Temperature}
	response, err := a.Client.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("agent %s: %w", d.Name, err)
	}
	findings, err := ParseFindings(response.Content)
	if err != nil {
		return nil, fmt.Errorf("agent %s: parse findings: %w", d.Name, err)
	}
	for i := range findings {
		findings[i].Agent = d.Name
		if findings[i].Severity == "" {
			findings[i].Severity = domain.Severity(d.Severity)
		}
		if findings[i].Severity == "" {
			findings[i].Severity = domain.SeverityInfo
		}
	}
	return findings, nil
}

func withDefaults(d Definition) Definition {
	if d.Name == "" {
		d.Name = "markdown-reviewer"
	}
	if d.System == "" {
		d.System = d.SystemPrompt
	}
	if d.System == "" {
		d.System = DefaultSystemPrompt
	}
	if d.Prompt == "" {
		d.Prompt = d.UserPrompt
	}
	if d.Prompt == "" {
		d.Prompt = d.ReviewPrompt
	}
	if d.MaxTokens <= 0 {
		d.MaxTokens = 2048
	}
	if d.Temperature == nil {
		v := 0.1
		d.Temperature = &v
	}
	if d.Enabled == nil {
		v := true
		d.Enabled = &v
	}
	return d
}

const DefaultSystemPrompt = "You are a careful senior code reviewer. Report only concrete, actionable issues introduced by this pull request. Do not invent facts."

// ParseFindings decodes the JSON array returned by a Markdown agent.
func ParseFindings(content string) ([]domain.Finding, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "[") {
		if start := strings.IndexByte(content, '['); start >= 0 {
			content = content[start:]
		}
	}
	if end := strings.LastIndexByte(content, ']'); end >= 0 {
		content = content[:end+1]
	}
	var findings []domain.Finding
	if err := json.Unmarshal([]byte(content), &findings); err != nil {
		return nil, err
	}
	return findings, nil
}

// ParseDefinition parses a Markdown document with optional YAML front matter.
func ParseDefinition(data []byte) (Definition, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return withDefaults(Definition{Prompt: strings.TrimSpace(text)}), nil
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return Definition{}, errors.New("agent: unterminated YAML front matter")
	}
	end += 4
	var d Definition
	if err := yaml.Unmarshal([]byte(text[4:end]), &d); err != nil {
		return Definition{}, fmt.Errorf("agent: parse front matter: %w", err)
	}
	d.Prompt = strings.TrimSpace(text[end+4:])
	return withDefaults(d), nil
}

func LoadFile(path string, client llm.Client) (*MarkdownAgent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := ParseDefinition(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &MarkdownAgent{Definition: d, Client: client}, nil
}

func LoadDir(dir string, client llm.Client) ([]*MarkdownAgent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []*MarkdownAgent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		agent, err := LoadFile(filepath.Join(dir, entry.Name()), client)
		if err != nil {
			return nil, err
		}
		if agent.Definition.Enabled != nil && !*agent.Definition.Enabled {
			continue
		}
		result = append(result, agent)
	}
	return result, nil
}

// DefaultDefinitions are useful in installations that do not provide files.
func DefaultDefinitions() []Definition {
	definitions := []Definition{
		{Name: "security", Description: "Find security vulnerabilities.", Prompt: "Look for authentication, authorization, injection, data exposure, and secret-handling issues.", Severity: string(domain.SeverityHigh)},
		{Name: "performance", Description: "Find performance regressions.", Prompt: "Look for avoidable latency, excessive resource use, inefficient algorithms, and scalability problems.", Severity: string(domain.SeverityMedium)},
		{Name: "coding-standards", Description: "Find coding-standard and best-practice issues.", Prompt: "Look for maintainability, readability, testing, error-handling, and established best-practice problems.", Severity: string(domain.SeverityMedium)},
	}
	for i := range definitions {
		definitions[i] = withDefaults(definitions[i])
	}
	return definitions
}

// BuiltinAgents constructs the default agents without requiring files on disk.
func BuiltinAgents(client llm.Client) []*MarkdownAgent {
	definitions := DefaultDefinitions()
	result := make([]*MarkdownAgent, len(definitions))
	for i := range definitions {
		result[i] = &MarkdownAgent{Definition: definitions[i], Client: client}
	}
	return result
}
