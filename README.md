# Anubis

Anubis reviews pull requests with a panel of specialist AI reviewers, then has a
coordinator model validate and deduplicate their findings into a single
high-signal review that it posts as a pull-request comment.

It is a static Go binary and a Docker-based GitHub Action. There is no server and
nothing to host.

## How it works

1. Anubis fetches the pull request and its diff from the GitHub REST API.
2. Four built-in specialists — `security`, `correctness`, `performance` and
   `maintainability` — review the diff **in parallel**, each with the same
   specialist system prompt and a different focus.
3. A coordinator model receives the diff plus every specialist's findings, then
   discards false positives, deduplicates by root cause, resolves disagreements,
   and re-grades severity.
4. The result is posted as a pull-request comment.

The coordinator is instructed to prefer omission over speculation, and to return
zero findings when that is the honest answer. A developer should be able to
trust that a published finding is real and actionable.

## GitHub Action

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

That is the whole setup. No `actions/checkout` step is needed.

### Inputs

| Input | Default | Purpose |
| --- | --- | --- |
| `github-token` | `${{ github.token }}` | Reads the PR and posts the comment. Needs `pull-requests: write`. |
| `repo` | `${{ github.repository }}` | Repository as `owner/name`. |
| `pr` | `${{ github.event.pull_request.number }}` | Pull request to review. |
| `publish` | `true` | Post the review as a PR comment. |
| `model` | `big-pickle` | Chat model identifier. |
| `llm-base-url` | `https://opencode.ai/zen/v1` | OpenAI-compatible `/chat/completions` base URL. |
| `github-base-url` | *(empty)* | GitHub API base URL. Set for GitHub Enterprise. |
| `opencode-api-key` | *(empty)* | Bearer token for the model endpoint. |
| `log-level` | `info` | `debug`, `info`, `warn` or `error`. |

### Notes and caveats

- **Secrets are not readable inside `action.yml`.** Pass the key with
  `with: opencode-api-key: ${{ secrets.OPENCODE_API_KEY }}`. Any
  OpenAI-compatible key works, including Gemini and Ollama.
- **Your diff is sent to the configured model endpoint.** On public
  repositories, assume the provider can see the change. Point `llm-base-url` at
  a self-hosted endpoint to keep it inside your infrastructure.
- **Fork pull requests do not get secrets.** `pull_request` from a fork leaves
  `opencode-api-key` empty, so the review fails. Use `pull_request_target` only
  if you understand that it grants the pull request write access to your
  repository.
- **GitHub builds the image from this repo's `Dockerfile` on every run.** A
  broken `Dockerfile` breaks every consumer, which is why CI builds it on each
  push.
- **Cost scales with agent count.** A review is 4 specialist calls plus 1
  synthesis call. `big-pickle` on OpenCode Zen's free tier is the default.

## CLI

```sh
make build
export GITHUB_TOKEN=...
export OPENCODE_API_KEY=...
./anubis -repo owner/project -pr 42 -publish
```

The CLI runs with no configuration: it defaults to OpenCode Zen's free
`big-pickle` model at `https://opencode.ai/zen/v1`.

```text
  -repo string         repository in 'owner/name' form   (env GITHUB_REPOSITORY)
  -pr int              pull request number              (required)
  -model string        chat model                       (env ANUBIS_MODEL)
  -llm-base-url string OpenAI-compatible base URL       (env ANUBIS_LLM_BASE_URL)
  -github-base-url     GitHub API base URL              (env GITHUB_API_URL)
  -github-token        GitHub token                     (env GITHUB_TOKEN)
  -publish             publish the review as a PR comment
  -log-level string    debug, info, warn, error         (env ANUBIS_LOG_LEVEL)
```

### Environment variables

| Variable | Purpose |
| --- | --- |
| `GITHUB_TOKEN` | Reads the PR and posts the comment. |
| `OPENCODE_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `AI_API_KEY` | Model provider credential, tried in that order. |
| `GITHUB_REPOSITORY` | Default for `-repo`. |
| `GITHUB_API_URL` | Default for `-github-base-url`. |
| `ANUBIS_MODEL`, `ANUBIS_LLM_BASE_URL`, `ANUBIS_LOG_LEVEL` | Defaults for the matching flags. |
| `ANUBIS_LOG_COLOR` | `always` / `never` to force or suppress ANSI color. `NO_COLOR` also works. |

### Other providers

Any OpenAI-compatible `/chat/completions` endpoint works. The key is sent as a
`Bearer` token, so provider keys drop straight in:

```sh
# Google Gemini
export OPENCODE_API_KEY=<gemini-api-key>
./anubis -repo owner/project -pr 42 \
  -llm-base-url https://generativelanguage.googleapis.com/v1beta/openai \
  -model gemini-2.5-flash

# Local Ollama
./anubis -repo owner/project -pr 42 \
  -llm-base-url http://localhost:11434/v1 -model qwen3-coder
```

For GitHub Enterprise, set `-github-base-url` to your API root, for example
`https://github.example.com/api/v3`.

## Limits and behavior worth knowing

- **Large diffs are truncated** at 200,000 bytes. The review then says so in the
  comment rather than implying full coverage.
- **Partial failure is disclosed.** If one specialist errors, the remaining
  findings are still synthesized and the comment states how many agents failed.
  If *every* agent fails, Anubis posts a warning that the review could not be
  completed, and exits non-zero.
- **A failed review never looks clean.** When the model provider errors, the
  comment says the review could not complete. It never posts a bare
  "No findings" as a result of an error.
- **A genuinely clean PR does get "No findings."**
- **Findings are Markdown, not structured JSON.** They are meant to be read by a
  human, not parsed by a tool.
- **`-log-level debug` never logs diff or prompt content.** Only sizes, counts
  and model metadata.
- **Max 4 agents run concurrently**, so a larger agent set cannot fan out into
  unbounded provider requests.

## Development

```sh
make build        # static ./anubis binary
make test         # full suite, including end-to-end tests
make e2e          # end-to-end tests only
make cover        # statement coverage
make fmt          # gofmt -w
make fmt-check    # fail if not gofmt-clean
make vet          # go vet
make lint         # golangci-lint
```

The test suite is fully hermetic: it drives the real compiled binary against
`httptest` mock servers for both the GitHub API and the model provider. No
network access, no secrets, no fixtures to maintain.

```sh
docker build -t anubis .        # must stay working; CI builds it on every push
```

## Security

Anubis treats the pull-request title, description and diff as **untrusted
input**. Reviewers are explicitly instructed that text inside the diff is data to
analyze, never instructions to follow, which is the main defense against prompt
injection through a malicious diff.

That is a mitigation, not a guarantee. The model still writes the comment, so
treat the output as untrusted until you have read it. See
[SECURITY.md](SECURITY.md) for reporting a vulnerability.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
