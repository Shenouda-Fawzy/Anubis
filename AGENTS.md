# AGENTS.md

Go pull-request review agent for GitHub. Module: `github.com/Shenouda-Fawzy/Anubis`; single entrypoint `cmd/anubis/main.go`.

## Commands

- Build: `make build` → static `./anubis` binary (`CGO_ENABLED=0`; ignored by git via `/anubis`).
- Verify: `make check` = gofmt clean check + `go vet` + `go test`.
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
- Models default to OpenCode Zen's free `big-pickle` model (`-llm-base-url` `https://opencode.ai/zen/v1`). Any OpenAI-compatible endpoint works, e.g. Google Gemini free tier (`https://generativelanguage.googleapis.com/v1beta/openai`). API key is `OPENCODE_API_KEY`, falling back to `OPENAI_API_KEY`, then `GEMINI_API_KEY`.
- Env: `GITHUB_TOKEN`, `OPENCODE_API_KEY`; flags `-repo`/`GITHUB_REPOSITORY`, `-github-base-url`/`GITHUB_API_URL`, `-model`/`ANUBIS_MODEL`, `-llm-base-url`/`ANUBIS_LLM_BASE_URL`, `-log-level`/`ANUBIS_LOG_LEVEL` (one of `debug`, `info`, `warn`, `error`; default `info`). Logs go to stderr as structured text via `log/slog`.
- `go.mod` requires the 1.26.x toolchain.

## GitHub Action packaging

- `action.yml` is a Docker-container action (`image: Dockerfile`); GitHub builds the image from `Dockerfile` at runtime. Keep `docker build .` working (CI checks it).
- Runtime image is `gcr.io/distroless/static:nonroot` (CA certs required for the GitHub/LLM HTTPS calls). The binary must stay fully static: `CGO_ENABLED=0`.
- `main.go`'s env defaults (`GITHUB_REPOSITORY`, `GITHUB_API_URL`, `GITHUB_TOKEN`) are what the container action relies on; `action.yml` passes `-pr`, `-publish`, `-agents` as `args`.
- CI (`.github/workflows/ci.yml`) runs `go test`, `gofmt -l`, `go vet`, and `docker build`; keep all four green.
- `examples/` holds consumer-facing copy-paste templates (GitHub Action + local `review.sh`); its `agents/*.md` mirror the root `agents/` files. `review.sh` needs `+x`.