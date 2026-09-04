package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/Shenouda-Fawzy/Anubis/pkg/domain"
	"github.com/Shenouda-Fawzy/Anubis/pkg/llm"
)

type fakeLLM struct {
	request llm.CompletionRequest
	reply   string
}

func (f *fakeLLM) Complete(_ context.Context, request llm.CompletionRequest) (llm.CompletionResponse, error) {
	f.request = request
	return llm.CompletionResponse{Content: f.reply}, nil
}

func TestParseDefinitionDefaultsAndAliases(t *testing.T) {
	definition, err := ParseDefinition([]byte("---\nname: api\nsystem_prompt: custom\nreview_prompt: check API\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if definition.Name != "api" || definition.System != "custom" || definition.Prompt != "check API" {
		t.Fatalf("unexpected definition: %+v", definition)
	}
	if definition.MaxTokens == 0 || definition.Enabled == nil || definition.Temperature == nil {
		t.Fatalf("defaults were not applied: %+v", definition)
	}
}

func TestMarkdownAgentReviewParsesFencedJSON(t *testing.T) {
	client := &fakeLLM{reply: "```json\n{\"findings\":[{\"title\":\"bug\",\"description\":\"bad\",\"severity\":\"high\",\"file\":\"x.go\",\"line\":3}]}\n```"}
	agent := &SubAgent{Definition: Definition{Name: "test", Prompt: "review"}, Client: client}
	findings, err := agent.Review(context.Background(), domain.ReviewInput{Diff: "diff"})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Agent != "test" || findings[0].Line != 3 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

func TestParseFindingsSupportsBareArrayAndWrappedObject(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"bare array", `[{"title":"a","description":"x","severity":"low"}]`, 1},
		{"wrapped object", `{"findings":[{"title":"a","description":"x","severity":"low"}]}`, 1},
		{"fenced wrapper", "```json\n{\"findings\":[{\"title\":\"a\",\"description\":\"x\",\"severity\":\"low\"}]}\n```", 1},
		{"empty object", `{"findings":[]}`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings, err := ParseFindings(tt.content)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != tt.want {
				t.Fatalf("got %d findings, want %d: %+v", len(findings), tt.want, findings)
			}
		})
	}
}

func TestMarkdownAgentReviewUsesStrictJSONSchema(t *testing.T) {
	client := &fakeLLM{reply: `{"findings":[]}`}
	agent := &SubAgent{Definition: Definition{Name: "test", Prompt: "review"}, Client: client}
	if _, err := agent.Review(context.Background(), domain.ReviewInput{Diff: "diff"}); err != nil {
		t.Fatal(err)
	}
	rf := client.request.ResponseFormat
	if rf == nil {
		t.Fatal("expected response_format to be set")
	}
	if rf.Type != "json_schema" {
		t.Fatalf("expected json_schema, got %q", rf.Type)
	}
	if rf.JSONSchema == nil {
		t.Fatal("expected json_schema payload")
	}
	if rf.JSONSchema.Strict == nil || !*rf.JSONSchema.Strict {
		t.Fatal("expected strict true")
	}
	if rf.JSONSchema.Name != "findings" {
		t.Fatalf("unexpected schema name %q", rf.JSONSchema.Name)
	}
	raw := string(rf.JSONSchema.Schema)
	for _, want := range []string{`"type":"object"`, `"findings"`, `"severity"`, `"title"`, `"description"`, `"confidence"`, `"critical"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("schema missing %q: %s", want, raw)
		}
	}
}

func TestParseFindingsToleratesStringConfidence(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  float64
	}{
		{"numeric string", `"0.9"`, 0.9},
		{"percentage", `"90%"`, 0.9},
		{"word high", `"high"`, 0.8},
		{"word medium", `"medium"`, 0.6},
		{"empty string", `""`, 0},
		{"bare number", `0.42`, 0.42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `{"findings":[{"title":"a","description":"x","severity":"low","confidence":` + tt.value + `}]}`
			findings, err := ParseFindings(content)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 1 {
				t.Fatalf("got %d findings, want 1: %+v", len(findings), findings)
			}
			if float64(findings[0].Confidence) != tt.want {
				t.Fatalf("confidence = %v, want %v", findings[0].Confidence, tt.want)
			}
		})
	}
}
