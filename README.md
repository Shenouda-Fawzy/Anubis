# Anubis

Anubis is a concurrent, extensible pull-request review agent. It runs Markdown
review agents against a GitHub diff, removes duplicate findings, asks an
OpenAI-compatible model for a concise synthesis, and can publish the result as
a pull-request comment.

## Quick start

```sh
make build          # static ./anubis binary (or: go run ./cmd/anubis ...)
export GITHUB_TOKEN=...
export OPENCODE_API_KEY=...
./anubis -repo owner/project -pr 42 -publish
```

Models come from [OpenCode Zen](https://opencode.ai/docs/zen/), with the free
`big-pickle` model as the default (`https://opencode.ai/zen/v1`). `OPENAI_API_KEY`
is still honored as a fallback. Point `-llm-base-url` at any OpenAI-compatible
endpoint (for example `http://localhost:11434/v1`) and select a model with
`-model`. Without `-agents`, the built-in definitions are used. To customize them:

```sh
go run ./cmd/anubis -repo owner/project -pr 42 -agents ./agents
```

### Other providers

Any OpenAI-compatible `/chat/completions` endpoint works. To avoid OpenCode
Zen rate limits, the Google Gemini free tier (key from
[Google AI Studio](https://aistudio.google.com)) is a drop-in:

```sh
export GITHUB_TOKEN=...
export OPENCODE_API_KEY=<your_gemini_api_key>   # sent as a Bearer token
go run ./cmd/anubis -repo owner/project -pr 42 \
  -llm-base-url https://generativelanguage.googleapis.com/v1beta/openai \
  -model gemini-2.5-flash  # or gemini-3.5-flash
```

`OPENAI_API_KEY` and `GEMINI_API_KEY` are also honored as Bearer-token
fallbacks. Use `-model`/`-llm-base-url` (or `ANUBIS_MODEL`/`ANUBIS_LLM_BASE_URL`)
for any other provider, e.g. Ollama at `http://localhost:11434/v1`.

## GitHub Action

Anubis ships as a Docker-container GitHub Action. Add it to any workflow that
handles `pull_request` events:

```yaml
name: Anubis review
on:
  pull_request:
permissions:
  pull-requests: write
  contents: read
jobs:
  anubis:
    runs-on: ubuntu-latest
    steps:
      - uses: Shenouda-Fawzy/Anubis@v1
        with:
          opencode-api-key: ${{ secrets.OPENCODE_API_KEY }}
```

The review is published as a PR comment by default; set `publish: false` to
opt out. No checkout is required for the built-in agents. To use custom
Markdown agents, add `actions/checkout@v4` and point `agents-dir` at a
directory committed to the consumer repo (for example `.github/anubis/agents`).

| Input | Default | Purpose |
| --- | --- | --- |
| `github-token` | `github.token` | Token; needs `pull-requests: write` to publish. |
| `repo` | `github.repository` | Repository as `owner/name`. |
| `pr` | PR number from the event | Pull request to review. |
| `publish` | `true` | Publish the review as a PR comment. |
| `agents-dir` | *(empty)* | Directory of Markdown agents; built-ins used when empty. |
| `model` | `big-pickle` | Chat model identifier. |
| `llm-base-url` | `https://opencode.ai/zen/v1` | OpenAI-compatible chat endpoint. |
| `github-base-url` | `github.api_url` | GitHub API base; set for GitHub Enterprise. |
| `opencode-api-key` | *(empty)* | Bearer token for the model endpoint. |

Notes:

- Secrets cannot be read inside `action.yml`; pass the API key via
  `with: opencode-api-key: ${{ secrets.OPENCODE_API_KEY }}` (or any
  OpenAI-compatible key / Gemini key).
- On `pull_request` from forks, repo secrets are not exposed. Use
  `pull_request_target` only if you need the action to run on fork PRs and
  understand the code-execution implications.
- GitHub builds the image from this repo's `Dockerfile` on each run (no
  registry needed). For a local smoke test:

  ```sh
  docker build -t anubis .
  docker run --rm \
    -e GITHUB_TOKEN=... -e GITHUB_REPOSITORY=owner/project \
    -e OPENCODE_API_KEY=... \
    anubis -pr 42 -publish=true
  ```

## Examples

Copy-paste ready templates live in [`examples/`](examples/):

- `examples/anubis-custom-agents.yml` — GitHub Action that reviews with custom
  Markdown agents loaded from a directory (`agents-dir`).
- `examples/agents/` — sample agent files to drop into your repo.
- `examples/review.sh` — run the same agents locally via the CLI.
- See `examples/README.md` for setup steps and the fork-PR caveat.

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

# Github settings
For Anubis to be able to drop comment on your PR you must have PAT (personal access token) with access to the repo(s) that are targeted for review. The PAT must have `pull requests` or `issues` permissions at least [RESTAPI](https://docs.github.com/en/rest/issues/comments?apiVersion=2026-03-10#create-an-issue-comment)
