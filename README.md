# Anubis

Anubis is a concurrent, extensible pull-request review agent. It runs Markdown
review agents against a GitHub diff, removes duplicate findings, asks an
OpenAI-compatible model for a concise synthesis, and can publish the result as
a pull-request comment.

## Quick start

```sh
export GITHUB_TOKEN=...
export OPENCODE_API_KEY=...
go run ./cmd/anubis -repo owner/project -pr 42 -publish
```

Models come from [OpenCode Zen](https://opencode.ai/docs/zen/), with the free
`big-pickle` model as the default (`https://opencode.ai/zen/v1`). `OPENAI_API_KEY`
is still honored as a fallback. Point `-llm-base-url` at any OpenAI-compatible
endpoint (for example `http://localhost:11434/v1`) and select a model with
`-model`. Without `-agents`, the built-in definitions are used. To customize them:

```sh
go run ./cmd/anubis -repo owner/project -pr 42 -agents ./agents
```

## Markdown agents

Each `.md` file may start with YAML front matter:

```markdown
---
name: api-review
description: Review API contracts
severity: medium
model: big-pickle
max_tokens: 2048
temperature: 0.1
enabled: true
---

Check compatibility, validation, and error handling in the changed API.
```

Missing values receive safe built-in defaults. The model must return a JSON
array of findings; fenced JSON is accepted too.

## Packages

- `pkg/domain`: review models and interfaces
- `pkg/agents`: Markdown agent loading and security/performance/coding-practice defaults
- `pkg/llm`: OpenAI-compatible chat adapter
- `pkg/orchestrator`: bounded concurrent execution, de-duplication, synthesis
- `pkg/github`: pull-request retrieval and comment publishing

Run the focused test suite with `go test ./...`; format changes with
`gofmt -w $(find cmd pkg -name '*.go')`.
