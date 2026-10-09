# Configuration

Every knob Anubis has, and what it defaults to. Nothing here is required — the
action works with a single input.

## Action inputs

Passed with `with:` in your workflow step.

[`examples/anubis.yml`](../examples/anubis.yml) is a complete workflow with all
eleven of these written out, if you would rather copy than assemble.

| Input | Default | Purpose |
| --- | --- | --- |
| `github-token` | `${{ github.token }}` | Reads the pull request and posts the comment. Needs `pull-requests: write`. |
| `repo` | `${{ github.repository }}` | Repository as `owner/name`. |
| `pr` | `${{ github.event.pull_request.number }}` | Pull request to review. |
| `publish` | `true` | Post the review as a PR comment. Set `false` to leave the PR untouched and keep the review in the job log. |
| `anubis-llm-model` | `big-pickle` | Chat model identifier. Must be served by the configured endpoint — see [Providers](providers.md). |
| `anubis-llm-base-url` | `https://opencode.ai/zen/v1` | OpenAI-compatible base URL. Anubis appends `/chat/completions` unless the value already ends in it. |
| `github-base-url` | *(empty)* | Leave empty. The action reads the correct API root from the runner, which is right for both github.com and GitHub Enterprise. |
| `anubis-llm-api-key` | *(empty)* | Bearer token for the model endpoint. Works with any OpenAI-compatible provider — see [Providers](providers.md) for each one's base URL. |
| `anubis-log-level` | `info` | `debug`, `info`, `warn` or `error`. Debug never logs diff content. |
| `anubis-max-concurrency` | `1` | How many specialists may run at once. `1` runs them one after another — see [Concurrency](behavior.md#concurrency). |
| `anubis-agents` | `false` | Load review agents from `.anubis-agents/` at the repository root on the default branch. See [Custom agents](behavior.md#custom-agents). |

## CLI flags

```text
  -repo string         repository in 'owner/name' form   (env GITHUB_REPOSITORY)
  -pr int              pull request number              (required)
  -model string        chat model                       (env ANUBIS_LLM_MODEL)
  -llm-base-url string OpenAI-compatible base URL       (env ANUBIS_LLM_BASE_URL)
  -github-base-url     GitHub API base URL              (env GITHUB_API_URL)
  -github-token        GitHub token                     (env GITHUB_TOKEN)
  -publish             publish the review as a PR comment
  -agents              load review agents from .anubis-agents   (env ANUBIS_AGENTS)
  -log-level string    debug, info, warn, error         (env ANUBIS_LOG_LEVEL)
```

Run `./anubis -h` for the same list with fuller descriptions.

## Environment variables

| Variable | Purpose |
| --- | --- |
| `GITHUB_TOKEN` | Reads the PR and posts the comment. |
| `ANUBIS_LLM_API_KEY` | Model provider credential. The documented name, and the only one Anubis reads. It does not name a provider, because Anubis speaks one protocol to all of them. |
| `GITHUB_REPOSITORY` | Default for `-repo`. |
| `GITHUB_API_URL` | Default for `-github-base-url`. |
| `ANUBIS_LLM_MODEL`, `ANUBIS_LLM_BASE_URL`, `ANUBIS_LOG_LEVEL` | Defaults for the matching flags. |
| `ANUBIS_MAX_CONCURRENCY` | How many specialists run at once. Default `1`. See [Concurrency](behavior.md#concurrency). |
| `ANUBIS_AGENTS` | When truthy, load review agents from `.anubis-agents/` on the default branch. Default `false`. See [Custom agents](behavior.md#custom-agents). |
| `ANUBIS_LOG_COLOR` | `always` or `never` to force or suppress ANSI color. `NO_COLOR` also works. |

Every flag has an environment variable except `-publish`, which defaults to
false for the CLI so a local run cannot post a comment by accident.

## Precedence

Flags beat environment variables, which beat the built-in defaults. In the
action, the `with:` inputs are mapped onto environment variables, so a flag
always wins over an input.

## GitHub Enterprise

Leave `github-base-url` empty. The action picks the API root up from the runner,
which is already correct on Enterprise.

Running the CLI outside Actions has no runner to ask, so pass the root
explicitly:

```sh
./anubis -repo owner/repo -pr 42 \
  -github-base-url https://github.example.com/api/v3
```

## Fork pull requests

`pull_request` from a fork does not expose repository secrets, so
`anubis-llm-api-key` arrives empty and the review fails with a clear error rather
than a confusing one.

`pull_request_target` does receive the secret, but it also grants the pull
request write access to your repository. Use it only if you understand that
trade-off.
