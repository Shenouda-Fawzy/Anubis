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

// Subagent follow OpenAI standard to define agent in markdown format 'agent.md'
type SubAgent struct {
	Definition Definition

	// Which LLM client this agent will be using
	Client llm.Client
}

func (a *SubAgent) IsSet() bool {
	if a == nil {
		return false
	}
	if *a.Definition.Enabled == false {
		return false
	}
	if a.Definition.Description == "" || a.Definition.Prompt == "" || a.Definition.Model == "" {
		return false
	}
	return true
}
func (a *SubAgent) Name() string { return a.Definition.Name }

// TODO:
//   - Remove structured response from sub-agents and keep in the master agent
//   - Correctly decode response in case of error
//   - Write unit and integration tests
//   - Cleanup the agents pkg
//   - Cleanup the LLM pkg
//   - Write default reliable agents (include resource manger agent)
//   - Write a reliable master agent
//   - Add Timeout for the HTTP clients
//   - Deploy on the marketplace
func (a *SubAgent) Review(ctx context.Context, input domain.ReviewInput) ([]domain.Finding, error) {
	if a == nil || a.Client == nil {
		return nil, errors.New("agent: an LLM client is required")
	}
	if a.IsSet() == false {
		return nil, errors.New("invalid agent, please ensure description, prompt and model are set")
	}
	d := a.Definition
	prompt := a.Definition.Prompt
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("agent %s: encode input: %w", d.Name, err)
	}
	req := llm.CompletionRequest{
		Model: d.Model,
		Messages: []llm.Message{
			{Role: "system", Content: d.System},
			{Role: "user", Content: prompt + "\n\nPull request context (JSON):\n" + string(payload) + "\n\nReturn only a single JSON object with a \"findings\" key holding a JSON array of findings. Each finding must include severity, title, description, and optionally file, line, end_line, suggestion, confidence. Do not wrap the response in prose or markdown fences."},
		},
		MaxTokens:      d.MaxTokens,
		Temperature:    d.Temperature,
		ResponseFormat: findingsResponseFormat(),
	}
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

// findingsResponseFormat builds a strict JSON Schema for the agent response so
// the LLM is constrained to return a well-formed {"findings": [...]} object.
func findingsResponseFormat() *llm.ResponseFormat {
	strict := true
	schema, err := json.Marshal(findingsSchema())
	if err != nil {
		return nil
	}
	return &llm.ResponseFormat{
		Type: "json_schema",
		JSONSchema: &llm.JSONSchema{
			Name:   "findings",
			Schema: schema,
			Strict: &strict,
		},
	}
}

func findingsSchema() map[string]any {
	finding := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"severity":    map[string]any{"type": "string", "enum": []string{"critical", "high", "medium", "low", "info"}},
			"title":       map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"file":        map[string]any{"type": "string"},
			"line":        map[string]any{"type": "integer"},
			"end_line":    map[string]any{"type": "integer"},
			"suggestion":  map[string]any{"type": "string"},
			"confidence":  map[string]any{"type": "number"},
		},
		"required":             []string{"severity", "title", "description"},
		"additionalProperties": false,
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"findings": map[string]any{
				"type":  "array",
				"items": finding,
			},
		},
		"required":             []string{"findings"},
		"additionalProperties": false,
	}
}

// ParseFindings decodes the JSON array returned by a Markdown agent. It
// accepts a bare JSON array ([...]) or a JSON object that wraps the array
// ({"findings":[...]}), and tolerates surrounding prose and ```json fences.
func ParseFindings(content string) ([]domain.Finding, error) {
	extracted := extractJSON(strings.TrimSpace(content))

	var findings []domain.Finding
	if err := json.Unmarshal([]byte(extracted), &findings); err == nil {
		return findings, nil
	}
	// Some JSON-mode endpoints wrap the array in an object like {"findings": [...]}.
	var wrapped struct {
		Findings []domain.Finding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(extracted), &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Findings, nil
}

// extractJSON pulls the outermost JSON document out of content, stripping
// markdown fences and leading/trailing prose but leaving the document intact.
func extractJSON(content string) string {
	if i := strings.Index(content, "```json"); i >= 0 {
		content = content[i+len("```json"):]
	} else if i := strings.Index(content, "```"); i >= 0 {
		content = content[i+len("```"):]
	}
	if i := strings.LastIndex(content, "```"); i >= 0 {
		content = content[:i]
	}
	content = strings.TrimSpace(content)

	start := strings.IndexByte(content, '{')
	if end := strings.IndexByte(content, '['); end >= 0 && (start < 0 || end < start) {
		start = end
	}
	if start < 0 {
		return content
	}

	var depth int
	var inString bool
	var escaped bool
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '"':
			if !escaped {
				inString = !inString
			}
			escaped = false
		case '\\':
			if inString {
				escaped = !escaped
			}
		case '{', '[':
			if !inString {
				depth++
			}
		case '}', ']':
			if !inString {
				depth--
				if depth == 0 {
					return content[start : i+1]
				}
			}
		}
	}
	return content[start:]
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

func LoadFile(path string, client llm.Client) (*SubAgent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := ParseDefinition(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &SubAgent{Definition: d, Client: client}, nil
}

func LoadDir(dir string, client llm.Client) ([]*SubAgent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []*SubAgent
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
func BuiltinAgents(client llm.Client) []*SubAgent {
	definitions := DefaultDefinitions()
	result := make([]*SubAgent, len(definitions))
	for i := range definitions {
		result[i] = &SubAgent{Definition: definitions[i], Client: client}
	}
	return result
}
