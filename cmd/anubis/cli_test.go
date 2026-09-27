package main

import (
	"errors"
	"strings"
	"testing"
)

// The provider variable a consumer sets must actually be the one Anubis reads.
// This failed silently before: the action exported OPENCODE_API_KEY while the
// code read only AI_API_KEY, so every published run died on a missing key.
func TestLLMAPIKeyFallbackOrder(t *testing.T) {
	all := []string{"OPENCODE_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "AI_API_KEY"}
	clearKeys(t, all...)

	cases := []struct {
		name string
		set  map[string]string
		want string
	}{
		{"none set", nil, ""},
		{"documented variable", map[string]string{"OPENCODE_API_KEY": "k1"}, "k1"},
		{"openai", map[string]string{"OPENAI_API_KEY": "k2"}, "k2"},
		{"gemini", map[string]string{"GEMINI_API_KEY": "k3"}, "k3"},
		{"legacy ai key", map[string]string{"AI_API_KEY": "k4"}, "k4"},
		{
			"precedence: opencode wins",
			map[string]string{"OPENCODE_API_KEY": "k1", "OPENAI_API_KEY": "k2", "GEMINI_API_KEY": "k3", "AI_API_KEY": "k4"},
			"k1",
		},
		{
			"precedence: openai over gemini",
			map[string]string{"OPENAI_API_KEY": "k2", "GEMINI_API_KEY": "k3", "AI_API_KEY": "k4"},
			"k2",
		},
		{
			"precedence: gemini over legacy",
			map[string]string{"GEMINI_API_KEY": "k3", "AI_API_KEY": "k4"},
			"k3",
		},
		{
			"empty value is skipped",
			map[string]string{"OPENCODE_API_KEY": "  ", "GEMINI_API_KEY": "k3"},
			"k3",
		},
		{"value is trimmed", map[string]string{"OPENCODE_API_KEY": "  k1  "}, "k1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearKeys(t, all...)
			for k, v := range tc.set {
				t.Setenv(k, v)
			}
			if got := llmAPIKey(); got != tc.want {
				t.Errorf("llmAPIKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

// clearKeys removes any inherited provider credentials for the duration of a
// test, so a developer's shell cannot influence the result.
func clearKeys(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

func TestFailureCommentIsNotACleanReview(t *testing.T) {
	got := failureComment(errBoom)
	if !strings.Contains(got, "not") || !strings.Contains(got, "clean review") {
		t.Errorf("failure comment must make clear the review did not pass:\n%s", got)
	}
	if !strings.Contains(got, errBoom.Error()) {
		t.Errorf("failure comment must include the error, got %q", got)
	}
}

func TestLongHelpMatchesDocumentedFlags(t *testing.T) {
	help := longHelp()
	for _, want := range []string{
		"-repo", "-pr", "-model", "-llm-base-url", "-github-base-url", "-github-token",
		"-publish", "-log-level", "OPENCODE_API_KEY", defaultModel, defaultLLMBaseURL,
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help text is missing %q", want)
		}
	}
	// The Markdown-agents feature was removed from the initial release; the help
	// text must not keep advertising it.
	if strings.Contains(help, "-agents") {
		t.Error("help text still advertises the removed -agents flag")
	}
}

func TestDefaultAgents(t *testing.T) {
	llm := &OpenAIClient{Model: "some-model"}
	as := defaultAgents(llm)

	if len(as) == 0 {
		t.Fatal("defaultAgents() returned no agents")
	}
	seen := map[string]bool{}
	for _, a := range as {
		if a.Name == "" {
			t.Error("agent has an empty name")
		}
		if seen[a.Name] {
			t.Errorf("duplicate agent name %q", a.Name)
		}
		seen[a.Name] = true
		if a.Description == "" {
			t.Errorf("agent %q has no focus description", a.Name)
		}
		if a.Model != "some-model" {
			t.Errorf("agent %q model = %q, want the client model", a.Name, a.Model)
		}
		if a.LlmClient != llm {
			t.Errorf("agent %q is not wired to the shared client", a.Name)
		}
		if len(as) > maxConcurrentAgents {
			t.Errorf("built-in agent count %d exceeds the concurrency limit %d", len(as), maxConcurrentAgents)
		}
	}
}

var errBoom = errors.New("provider exploded")
