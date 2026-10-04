package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// providerNamedKeyVars are the credentials Anubis used to fall back through
// before the variable was renamed. They must stay unread: a key that works only
// when the user guesses a provider name is a bug, not a convenience.
var providerNamedKeyVars = []string{
	"OPENCODE_API_KEY",
	"OPENAI_API_KEY",
	"GEMINI_API_KEY",
	"AI_API_KEY",
}

// The variable a consumer sets must be the one Anubis reads. This failed
// silently once already: the action exported OPENCODE_API_KEY while the code
// read only AI_API_KEY, so every published run died on a missing key.
func TestLLMAPIKeyReadsDocumentedVariable(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want string
	}{
		{"unset", nil, ""},
		{"empty", map[string]string{"ANUBIS_LLM_API_KEY": ""}, ""},
		{"whitespace only", map[string]string{"ANUBIS_LLM_API_KEY": "  \t "}, ""},
		{"documented variable", map[string]string{"ANUBIS_LLM_API_KEY": "k0"}, "k0"},
		{"value is trimmed", map[string]string{"ANUBIS_LLM_API_KEY": "  k0  "}, "k0"},
		{
			"provider-named variable alone does not count",
			map[string]string{"OPENAI_API_KEY": "k2", "GEMINI_API_KEY": "k3"},
			"",
		},
		{
			"documented variable wins over a provider-named one",
			map[string]string{"ANUBIS_LLM_API_KEY": "k0", "OPENAI_API_KEY": "k2"},
			"k0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearKeys(t, "ANUBIS_LLM_API_KEY")
			clearKeys(t, providerNamedKeyVars...)
			for k, v := range tc.set {
				t.Setenv(k, v)
			}
			if got := llmAPIKey(); got != tc.want {
				t.Errorf("llmAPIKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Reading a provider-named variable is the failure mode this rename exists to
// remove, and it is invisible until a user on a non-default provider cannot work
// out which variable to set. Asserting the absence keeps it gone.
func TestLLMAPIKeyIgnoresProviderNamedVariables(t *testing.T) {
	t.Setenv("ANUBIS_LLM_API_KEY", "")
	for _, name := range providerNamedKeyVars {
		t.Setenv(name, "should-be-ignored")
	}
	if got := llmAPIKey(); got != "" {
		t.Errorf("llmAPIKey() = %q, want empty: a provider-named variable supplied the key", got)
	}
}

// Anubis speaks one protocol to many providers, so the credential it asks for
// must carry the project prefix and name no product.
func TestDocumentedAPIKeyIsProviderAgnostic(t *testing.T) {
	const name = "ANUBIS_LLM_API_KEY"
	if llmAPIKeyEnvVar != name {
		t.Errorf("llmAPIKeyEnvVar = %q, want %q", llmAPIKeyEnvVar, name)
	}
	if !strings.HasPrefix(llmAPIKeyEnvVar, "ANUBIS_") {
		t.Errorf("credential variable %q is not prefixed ANUBIS_", llmAPIKeyEnvVar)
	}
	for _, banned := range []string{"OPEN", "GEMINI", "GROQ", "OPENROUTER", "ANTHROPIC", "DEEPSEEK", "MISTRAL", "XAI", "GOOGLE"} {
		if strings.Contains(llmAPIKeyEnvVar, banned) {
			t.Errorf("credential variable %q names the provider %q", llmAPIKeyEnvVar, banned)
		}
	}
	if !strings.Contains(longHelp(), "ANUBIS_LLM_API_KEY") {
		t.Error("help text does not document ANUBIS_LLM_API_KEY")
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
		"-publish", "-log-level", "ANUBIS_LLM_API_KEY", defaultModel, defaultLLMBaseURL,
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

func TestMaxConcurrencyFromEnv(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
		want int
	}{
		{name: "unset runs sequentially", set: false, want: 1},
		{name: "empty runs sequentially", set: true, val: "", want: 1},
		{name: "explicit one", set: true, val: "1", want: 1},
		{name: "two", set: true, val: "2", want: 2},
		{name: "four", set: true, val: "4", want: 4},
		{name: "surrounding whitespace", set: true, val: "  3  ", want: 3},
		{name: "large value is not clamped here", set: true, val: "1000", want: 1000},
		{name: "zero falls back", set: true, val: "0", want: 1},
		{name: "negative falls back", set: true, val: "-2", want: 1},
		{name: "not a number falls back", set: true, val: "abc", want: 1},
		{name: "float falls back", set: true, val: "2.5", want: 1},
		{name: "junk falls back", set: true, val: "4;rm -rf /", want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("ANUBIS_MAX_CONCURRENCY", tc.val)
			} else {
				if err := os.Unsetenv("ANUBIS_MAX_CONCURRENCY"); err != nil {
					t.Fatalf("Unsetenv() error = %v", err)
				}
			}
			if got := maxConcurrency(); got != tc.want {
				t.Errorf("maxConcurrency() = %d, want %d", got, tc.want)
			}
		})
	}
}

// The help text is the only place a user learns the default, so it has to agree
// with the code.
func TestLongHelpDocumentsMaxConcurrency(t *testing.T) {
	help := longHelp()
	if !strings.Contains(help, "ANUBIS_MAX_CONCURRENCY") {
		t.Error("longHelp() does not document ANUBIS_MAX_CONCURRENCY")
	}
	if !strings.Contains(help, "default 1") {
		t.Error("longHelp() does not state the default of 1")
	}
}

// A workflow input that is left unset expands to an empty string, and the
// action passes it through. Without this, a non-pull_request event produced a
// raw strconv error and a usage dump instead of the required-flag message.
func TestOptionalIntAcceptsEmptyValue(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{"42", 42},
		{" 42 ", 42},
		{"0", 0},
		{"-1", -1},
	}
	for _, tc := range tests {
		var got int
		f := optionalInt{&got}
		if err := f.Set(tc.in); err != nil {
			t.Errorf("Set(%q) returned %v, want nil", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Set(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestOptionalIntRejectsNonNumeric(t *testing.T) {
	var got int
	if err := (optionalInt{&got}).Set("abc"); err == nil {
		t.Error(`Set("abc") returned nil, want a parse error`)
	}
}

func TestOptionalIntString(t *testing.T) {
	var got int
	f := optionalInt{&got}
	if s := f.String(); s != "0" {
		t.Errorf("String() on unset = %q, want %q", s, "0")
	}
	if err := f.Set("7"); err != nil {
		t.Fatalf("Set(7) returned %v", err)
	}
	if s := f.String(); s != "7" {
		t.Errorf("String() = %q, want %q", s, "7")
	}
	if s := (optionalInt{nil}).String(); s != "" {
		t.Errorf("String() on nil target = %q, want empty", s)
	}
}
