package agents

import (
	"context"
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
	client := &fakeLLM{reply: "```json\n[{\"title\":\"bug\",\"description\":\"bad\",\"severity\":\"high\",\"file\":\"x.go\",\"line\":3}]\n```"}
	agent := &MarkdownAgent{Definition: Definition{Name: "test", Prompt: "review"}, Client: client}
	findings, err := agent.Review(context.Background(), domain.ReviewInput{Diff: "diff"})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Agent != "test" || findings[0].Line != 3 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}
