# AGENTS.md

Guidance for coding agents working in this repository.

## Commands

- Build: `make build` → static `./anubis` binary (`CGO_ENABLED=0 -trimpath`;
  ignored by git via `/anubis`).
- Test: `make test` → `go test ./...`. Fully hermetic: `httptest` mock servers
  for both the GitHub API and the model provider, no env vars or network.
- E2E only: `make e2e`. The E2E tests build the real binary in `TestMain` and
  drive it against mock servers.
- Coverage: `make cover`.
- Format: `make fmt`. Gate: `make fmt-check` (must report nothing).
- Vet: `make vet`. Lint: `make lint` (golangci-lint; see `.golangci.yml`).
- CI (`.github/workflows/ci.yml`) runs `gofmt -l cmd`, `go vet`, `go test -race`,
  `golangci-lint` and `docker build`. Keep all five green.
- The only third-party dependency is `github.com/google/go-github/v90`. Prefer the
  standard library; do not add dependencies without a reason worth stating in
  the PR.

## Architecture

Everything lives in `package main` under `cmd/anubis`. There is no `pkg/` tree.

| File | Responsibility |
| --- | --- |
| `main.go` | `main`, defaults (`defaultModel`, `defaultLLMBaseURL`, `httpTimeout`, `maxDiffBytes`), slog setup, `fatal` |
| `cli.go` | Flag parsing, orchestration of the run, step outputs, published comment |
| `github.go` | PR/diff retrieval and comment publishing over the GitHub REST API |
| `openai.go` | OpenAI-compatible `/chat/completions` client and response parsing |
| `completion.go` | Request/response wire types shared by agents and the coordinator |
| `subagent_new.go` | `Agent`, the `ChatCompleter` interface, per-agent review |
| `subagentprompt.go` | Specialist system prompt and the per-agent task renderer |
| `coordinator.go` | Concurrent agent fan-out, synthesis call, result state |
| `masterprompt.go` | Coordinator system prompt |
| `reviewprompt.go` | Coordinator user-message template |
| `log_color.go` | Optional ANSI coloring of the slog text output |

Flow: load PR and diff → run all agents (concurrency from `ANUBIS_MAX_CONCURRENCY`,
default 1, hard ceiling 4) → synthesize with the coordinator → optionally post as
a PR comment.

## Non-obvious behavior

- **There is exactly one credential variable: `ANUBIS_LLM_API_KEY`.** It is a bare
  `os.Getenv` in `llmAPIKey` (`cli.go`), paired with the `anubis-llm-api-key`
  action input. Earlier versions fell back through `OPENCODE_API_KEY`,
  `OPENAI_API_KEY`, `GEMINI_API_KEY` and `AI_API_KEY`; that chain is gone and must
  not come back. `TestLLMAPIKeyIgnoresProviderNamedVariables` fails if any
  provider-named variable ever supplies a key again. If you rename the variable,
  change `cli.go`, `action.yml`, `docs/configuration.md`, the help text and
  `TestLLMAPIKeyReadsDocumentedVariable` together.
- **Every variable Anubis owns is prefixed `ANUBIS_`.** Nothing in the codebase may
  name a model provider in a variable, flag or input: one protocol, many
  providers, and the credential is the provider's key whoever serves it. The
  `GITHUB_*` names are the deliberate exception — they are GitHub's own contract,
  set by the runner and read by convention across the ecosystem.
- **Provider support is a documentation claim, not a code path.** Anubis has no
  per-provider logic; `docs/providers.md` lists base URLs that were checked against
  each vendor's documentation. Those URLs go stale — GitHub Models was retired in
  July 2026 — so re-verify before adding a row and never present a row as tested
  when it was only read.
- **The coordinator template has six verbs and six arguments.** `reviewPrompt`
  is formatted with repo, PR number, title, description, diff, findings. A short
  argument list silently shifts every value and injects `%!s(MISSING)`.
  `TestReviewPromptArgumentsAreAligned` exists to catch exactly that.
- **Agents receive the diff; the coordinator prompt is not for them.** Each
  specialist gets `subAgentPrompt` as its system message and a task rendered by
  `subAgentTask` that embeds the PR context and the full diff. `Agent.Review`
  takes a non-empty `pr.Diff` and returns an error otherwise.
- **Agents may run concurrently, but findings are folded in configured order**
  (`Coordinator.runAgents`), so the synthesis prompt is stable across runs. Each
  goroutine writes only its own `Agent.Finding`; run `go test -race` if you touch
  this. The default is **sequential** (`defaultMaxConcurrency = 1`) to avoid
  rate limits; `Coordinator.MaxConcurrency` raises it, and the `concurrency()`
  helper clamps it to the agent count and to `maxConcurrentAgents` (4).
- **Partial failure is tolerated, total failure is not.** One failed agent still
  produces a review, and the comment discloses how many agents failed. If no
  agent produced a finding, `Review` returns an error and the comment becomes a
  "could not complete" warning. A review that errored must never look clean.
- **Findings are free-form Markdown.** There is no JSON schema, no severity
  field and no `Approved` flag in the code. The `critical` / `warning` /
  `suggestion` levels exist only as prompt instructions.
- **Diffs are truncated at `maxDiffBytes`** with an inline marker; the published
  comment discloses the truncation.
- **Debug logs never include diff or prompt content.** Only byte counts, token
  counts and model metadata. Keep it that way: on a public repo the diff is
  source code, and findings can echo secrets found in it.
- **Model defaults are OpenCode Zen's free `big-pickle`** at
  `https://opencode.ai/zen/v1`. Any OpenAI-compatible endpoint works, including
  a local Ollama at `http://localhost:11434/v1`.
- **The request body is only `model` + `messages`.** No `temperature`,
  `max_tokens`, `stream` or `response_format`, and `finish_reason` must be exactly
  `"stop"` or the response is discarded as truncated. This is deliberate: it keeps
  the compatibility surface small, and it means truncation comes from the model's
  context window rather than a cap we set.
- **Logs go to stderr** via `log/slog` as structured text. ANSI color only for a
  TTY unless forced with `ANUBIS_LOG_COLOR=always`; `NO_COLOR` disables it.
- **Markdown-defined agents are not part of the initial release.** There is no
  `-agents` flag, no front-matter parsing and no `pkg/agents` loader. Do not
  reintroduce them without a design discussion.
- `go.mod` requires the 1.26.x toolchain.

## GitHub Action packaging

- `action.yml` is a Docker-container action (`image: Dockerfile`); GitHub builds
  the image from `Dockerfile` at run time. Keep `docker build .` working, because
  a broken build breaks every consumer. CI builds it on each push.
- The runtime image is `gcr.io/distroless/static:nonroot`. CA certificates are
  required for the GitHub and LLM HTTPS calls, so keep them in the image.
- The binary must stay fully static: `CGO_ENABLED=0`.
- `cli.go`'s env defaults (`GITHUB_REPOSITORY`, `GITHUB_API_URL`, `GITHUB_TOKEN`,
  `ANUBIS_*`) are what the container action relies on; `action.yml` passes
  `-pr`, `-publish` and `-log-level` as args.
- `examples/` holds consumer-facing copy-paste templates. `review.sh` must stay
  executable.
