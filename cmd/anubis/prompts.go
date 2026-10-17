package main

import _ "embed"

// The review prompts are authored as Markdown and embedded into the binary at
// build time, so the distroless runtime image needs no source tree and the
// prompts live as reviewable files rather than Go string literals.
//
// Each specialist file is the full system prompt for one agent; defaultAgents
// wires them together. The master file is the coordinator's system prompt.

//go:embed specialist/security-agent.md
var securityAgentPrompt string

//go:embed specialist/correctness-agent.md
var correctnessAgentPrompt string

//go:embed specialist/performance-agent.md
var performanceAgentPrompt string

//go:embed specialist/maintainability-agent.md
var maintainabilityAgentPrompt string

//go:embed specialist/master-agent.md
var masterPrompt string
