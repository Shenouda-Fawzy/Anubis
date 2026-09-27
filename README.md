# Anubis

Anubis reviews pull requests with a panel of specialist AI reviewers, then has a
coordinator model validate and deduplicate their findings into a single
high-signal review that it posts as a pull-request comment.

It is a static Go binary and a Docker-based GitHub Action. There is no server and
nothing to host.

## How it works

1. Anubis fetches the pull request and its diff from the GitHub REST API.
2. Four built-in specialists — `security`, `correctness`, `performance` and
   `maintainability` — review the diff, each with the same specialist system
   prompt and a different focus. They run **one at a time** by default; see
   [Concurrency](#concurrency) to change that.
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
- **Your diff is sent to the configured model endpoint, and the default model is
  not zero-retention.** The full diff is uploaded on every review. OpenCode
  states that `big-pickle` data may be used to improve the model during its free
  period. Point `llm-base-url` at a self-hosted endpoint to keep the diff
  inside your infrastructure. See
  [Where your diff goes](SECURITY.md#where-your-diff-goes).
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

For GitHub Enterprise, leave `github-base-url` empty. The action reads the API
root from the runner, so Enterprise works with no configuration. When running
the CLI by hand outside Actions, pass the API root explicitly:

```sh
./anubis -repo owner/project -pr 42 \
  -github-base-url https://github.example.com/api/v3
```

### Model compatibility

Anubis speaks one protocol: OpenAI's `POST {base}/chat/completions`. It builds
the URL from `-llm-base-url` and appends `/chat/completions` unless the value
already ends in it, so all of these work:

```
https://opencode.ai/zen/v1                  -> .../v1/chat/completions
https://opencode.ai/zen/v1/                 -> .../v1/chat/completions
https://opencode.ai/zen/v1/chat/completions -> used as-is
```

**This matters for OpenCode Zen, which routes per model.** Zen does not serve
every model from one endpoint:

| Model family on Zen | Endpoint | Works with Anubis |
| --- | --- | --- |
| `big-pickle`, `qwen3.8-max`, `deepseek-v4*`, `glm-*`, `kimi-*`, `minimax-*` | `/zen/v1/chat/completions` | Yes |
| `gpt-*`, `grok-*`, `muse-spark-*` | `/zen/v1/responses` | No |
| `claude-*`, `qwen3.*-flash/plus` | `/zen/v1/messages` | No |
| `gemini-*` | `/zen/v1/models/<id>` | No |

Selecting a model from a row marked **No** will fail, because the request goes
to `/chat/completions` and that model is not served there. Check
<https://opencode.ai/docs/zen/#endpoints> before switching `-model` on Zen.

To reach those models, point `-llm-base-url` at the matching path yourself and
be aware that Anubis still speaks the chat-completions request and response
shape, so it only works where that shape is also correct.

### Concurrency

By default the four specialists run **one after another**. That is deliberate: a
review costs one provider call per specialist, so running them together is the
fastest way to exhaust a free tier's rate limit.

Set `ANUBIS_MAX_CONCURRENCY` (or the action's `max-concurrency` input) to raise
it when your provider can absorb the fan-out and you want the wall-clock time
back:

```yaml
      - uses: Shenouda-Fawzy/Anubis@v1
        with:
          opencode-api-key: ${{ secrets.OPENCODE_API_KEY }}
          max-concurrency: '4'
```

```sh
ANUBIS_MAX_CONCURRENCY=4 ./anubis -repo owner/project -pr 42
```

The value is clamped to the number of agents and to a hard ceiling of 4, so a
large setting cannot produce unbounded parallel requests. An unset,
unparseable or non-positive value is treated as `1` and logged as a warning,
because a typo in a tuning variable should not stop a review. Findings are
collected by agent index, so the synthesis prompt is identical whether the
specialists ran in parallel or in sequence.

Raising this does not reduce cost. It spends the same tokens in less time, which
makes hitting a rate limit *more* likely.

### Cost

`big-pickle` is currently free. Zen also **auto-reloads $20 when a balance drops
below $5**, and that is on by default. Free models will not trigger it, but if
you switch `-model` to a paid one, confirm your Zen auto-reload setting first.

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
- **Specialists run one at a time by default.** A review costs one provider
  call per specialist, so the shipped default of `1` is the gentlest option for
  a rate limit. See [Concurrency](#concurrency).

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
treat the output as untrusted until you have read it.

Note that the default model is **not** zero-retention — see
[Where your diff goes](SECURITY.md#where-your-diff-goes) before pointing this at
a private repository. See [SECURITY.md](SECURITY.md) for reporting a
vulnerability.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
