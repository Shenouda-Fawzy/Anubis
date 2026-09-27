# Development

## Build and test

```sh
make build        # static ./anubis binary
make test         # full suite, including end-to-end tests
make e2e          # end-to-end tests only
make cover        # statement coverage
make fmt          # gofmt -w
make fmt-check    # fail if not gofmt-clean
make vet          # go vet
make lint         # golangci-lint
make tidy         # go mod tidy
```

The test suite is **fully hermetic**: it drives the real compiled binary against
`httptest` mock servers for both the GitHub API and the model provider. No
network access, no secrets, no fixtures to maintain.

## Docker

```sh
docker build -t anubis .
```

This must keep working. GitHub builds the image from this repo's `Dockerfile` on
every action run, so a broken `Dockerfile` breaks every consumer. CI builds it on
each push for that reason.

The runtime image is `gcr.io/distroless/static:nonroot` and the binary is fully
static (`CGO_ENABLED=0`). Both are required: the action runs in that image, and
CA certificates are needed for the GitHub and model HTTPS calls.

## Code layout

Everything lives in `package main` under `cmd/anubis`. There is no `pkg/` tree.

| File | Responsibility |
| --- | --- |
| `main.go` | Defaults, slog setup, `fatal`, `envOr`, concurrency resolution |
| `cli.go` | Flag parsing, orchestration, published comment |
| `github.go` | PR and diff retrieval, comment publishing |
| `openai.go` | OpenAI-compatible `/chat/completions` client and response parsing |
| `completion.go` | Request and response wire types |
| `coordinator.go` | Agent fan-out, synthesis call, result state |
| `subagent_new.go` | `Agent`, the `ChatCompleter` interface, per-agent review |
| `subagentprompt.go` | Specialist system prompt and per-agent task renderer |
| `masterprompt.go` | Coordinator system prompt |
| `reviewprompt.go` | Coordinator user-message template |
| `log_color.go` | Optional ANSI coloring of the slog text output |

## Dependencies

`github.com/google/go-github/v90` is the only one. Prefer the standard library;
a new dependency needs a reason stated in the PR.

`go.mod` requires the 1.26.x toolchain.

## Gotchas

These are the things that break quietly if you are not careful. `AGENTS.md` in
the repository root carries the full list.

- **The API key is a fallback chain, not one variable.** `OPENCODE_API_KEY` →
  `OPENAI_API_KEY` → `GEMINI_API_KEY` → `AI_API_KEY`. Change it in `cli.go`,
  `action.yml`, the docs table and `TestLLMAPIKeyFallbackOrder` together.
- **The synthesis template has six verbs and six arguments.** A short argument
  list silently shifts every value and injects `%!s(MISSING)`.
  `TestReviewPromptArgumentsAreAligned` exists to catch that.
- **Never log diff or prompt content.** On a public repository the diff is
  source code.
- **Findings are free-form Markdown.** The `critical` / `warning` / `suggestion`
  levels exist only as prompt instructions, not as fields in code.
- **Findings must stay in configured order.** Each goroutine writes only its own
  slot; run `go test -race` if you touch `runAgents`.
- **`docker build` must work.** It is the only thing that catches a broken
  dependency, because a doctored module cache compiles fine locally.
