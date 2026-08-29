# AGENTS.md

Go pull-request review agent for GitHub. Module: `github.com/Shenouda-Fawzy/Anubis`; single entrypoint `cmd/anubis/main.go`.

## Commands

- Test: `go test ./...` — fully hermetic (httptest servers, fake LLM clients); no env vars or network needed.
- Format: `gofmt -w $(find cmd pkg -name '*.go')`; keep `gofmt -l cmd pkg` empty.
- Only dependency is `gopkg.in/yaml.v3`; don't pull in new third-party deps without reason.

## Architecture

- `pkg/domain`: shared types (`Finding`, `Review`, `Agent` interface).
- `pkg/agents`: Markdown-defined review agents; `agents/*.md` are the example files selected with `-agents ./agents`.
- `pkg/llm`: OpenAI-compatible `/chat/completions` client; local endpoints via `-llm-base-url` (e.g. `http://localhost:11434/v1`).
- `pkg/orchestrator`: runs all agents concurrently (max 4), preserves agent order, dedupes, then LLM-synthesizes a summary.
- `pkg/github`: PR/diff retrieval and comment publishing over the REST API (`X-GitHub-Api-Version: 2022-11-28`).

Flow: load agents → each returns `[]Finding` → dedupe → summary synthesis → optional `-publish` as a PR comment.

## Non-obvious behavior

- Findings are deduped by `ID`, else lowercased `file|line|end_line|title`; same bug reported by multiple agents collapses to one.
- `Review.Approved` is false iff any finding has severity `critical` or `high`.
- Agent markdown has optional YAML front matter starting exactly with `---\n`. Alias fields: `system`↔`system_prompt`, `prompt`↔`user_prompt`↔`review_prompt`. `enabled: false` drops the file. Missing values get safe defaults (`withDefaults` in `pkg/agents/markdown.go`).
- Models must return a JSON array of findings; `ParseFindings` tolerates ```json fences and surrounding prose.
- If `-agents` is unset or the dir yields no enabled agents, built-in agents (security/performance/coding-standards) run instead.
- Models default to OpenCode Zen's free `big-pickle` model (`-llm-base-url` `https://opencode.ai/zen/v1`, `OPENCODE_API_KEY`, fallback `OPENAI_API_KEY`). See `llm.DefaultBaseURL`/`llm.DefaultModel`.
- Env: `GITHUB_TOKEN`, `OPENCODE_API_KEY`; flags `-repo`/`GITHUB_REPOSITORY`, `-github-base-url`/`GITHUB_API_URL`, `-model`/`ANUBIS_MODEL` (default `big-pickle`), `-llm-base-url`/`ANUBIS_LLM_BASE_URL` (default `https://opencode.ai/zen/v1`).
- `go.mod` requires the 1.26.x toolchain.