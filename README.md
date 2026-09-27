# Anubis

Anubis reviews pull requests with a panel of specialist AI reviewers, then has a
coordinator validate and deduplicate their findings into one high-signal review
that it posts as a pull-request comment.

Static Go binary, Docker-based GitHub Action. No server, nothing to host.

## As an action

```yaml
name: Anubis review
on: [pull_request]
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

That is the whole setup — no `actions/checkout` step needed.

## As a CLI

```sh
make build
export GITHUB_TOKEN=...
export OPENCODE_API_KEY=...

./anubis -repo owner/repo -pr 42 -publish
```

Zero configuration. It defaults to OpenCode Zen's free `big-pickle` model.

## What it does

Four specialists — `security`, `correctness`, `performance` and
`maintainability` — review the diff one at a time, each with a different focus. A
coordinator then discards false positives, deduplicates by root cause, and
re-grades severity. The result is posted as a comment.

## Common changes

| To do this | Set this |
| --- | --- |
| Use another model or provider | `model`, `llm-base-url` |
| Run the reviewers in parallel | `max-concurrency` |
| Keep the review out of the PR | `publish: false` |
| See more detail in the log | `log-level: debug` |

Every input, flag and environment variable is in
[docs/configuration.md](docs/configuration.md).

## Documentation

| Page | Covers |
| --- | --- |
| [Configuration](docs/configuration.md) | Every input, flag and env var. |
| [Providers](docs/providers.md) | Which models work, cost, where your diff goes. |
| [Behavior](docs/behavior.md) | Truncation, partial failure, logging, concurrency. |
| [Development](docs/development.md) | Building, testing, code layout. |

## Security

The diff is untrusted input and is uploaded to whichever model endpoint you
configure. **The default model is not zero-retention** — read
[docs/providers.md](docs/providers.md#where-your-diff-goes) before pointing this
at a private repository. See [SECURITY.md](SECURITY.md) to report a
vulnerability.

## License

[MIT](LICENSE)
