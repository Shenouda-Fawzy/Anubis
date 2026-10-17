package main

import "testing"

// The review prompts are embedded from Markdown at build time. A bad embed path
// fails to compile, but this also guards against an accidentally empty or
// copy-pasted prompt file.
func TestEmbeddedPromptsArePresentAndDistinct(t *testing.T) {
	prompts := map[string]string{
		"security":        securityAgentPrompt,
		"correctness":     correctnessAgentPrompt,
		"performance":     performanceAgentPrompt,
		"maintainability": maintainabilityAgentPrompt,
		"master":          masterPrompt,
	}

	seen := map[string]string{}
	for name, prompt := range prompts {
		if prompt == "" {
			t.Errorf("%s prompt is empty", name)
			continue
		}
		if other, dup := seen[prompt]; dup {
			t.Errorf("%s prompt is identical to the %s prompt", name, other)
		}
		seen[prompt] = name
	}
}
