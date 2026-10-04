# Anubis

[![CI](https://github.com/Shenouda-Fawzy/Anubis/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Shenouda-Fawzy/Anubis/actions/workflows/ci.yml)
[![action version](https://img.shields.io/github/v/release/Shenouda-Fawzy/Anubis?label=action%20v1)](https://github.com/Shenouda-Fawzy/Anubis/releases/latest)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Anubis reviews pull requests with a panel of specialist AI reviewers, then has a
coordinator validate and deduplicate their findings into one high-signal review
that it posts as a pull-request comment.

Static Go binary, Docker-based GitHub Action. No server, nothing to host.

## As an action

The minimum that works. Copy this into `.github/workflows/anubis.yml`:

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
          anubis-llm-api-key: ${{ secrets.ANUBIS_LLM_API_KEY }}
```

Two things to set up, and one of them is GitHub's job, not yours:

1. Add **one** secret, `ANUBIS_LLM_API_KEY`, holding your model provider's key
   (Settings → Secrets and variables → Actions).
2. Nothing for the GitHub token. It defaults to `${{ github.token }}`, which
   `pull-requests: write` above is enough to post a comment with.

Everything else has a default: the model is OpenCode Zen's free `big-pickle`,
the pull request is whichever one triggered the run, and the review is posted
as a comment. No `actions/checkout` step, because Anubis fetches the diff over
the API rather than from a working tree.

**[`examples/anubis.yml`](examples/anubis.yml) is the complete configuration** —
all ten inputs written out, including the GitHub token, the model and the
endpoint, with the reasoning for each in comments. That file is the reference;
this page keeps only the minimum you need to get the first review.

## As a CLI

```sh
make build
export GITHUB_TOKEN=...
export ANUBIS_LLM_API_KEY=...

./anubis -repo owner/repo -pr 42 -publish
```

Zero configuration. It defaults to OpenCode Zen's free `big-pickle` model.

## What it does

Four specialists — `security`, `correctness`, `performance` and
`maintainability` — review the diff one at a time, each with a different focus. A
coordinator then discards false positives, deduplicates by root cause, and
re-grades severity. The result is posted as a comment.

## Providers

Anubis speaks one protocol — OpenAI's `POST {base}/chat/completions` — so switching
provider is a URL and a key. No adapter, no code change:

```yaml
- uses: Shenouda-Fawzy/Anubis@v1
  with:
    anubis-llm-api-key: ${{ secrets.ANUBIS_LLM_API_KEY }}
    llm-base-url: https://api.deepseek.com   # any OpenAI-compatible endpoint
    model: deepseek-chat
```

| Provider | `llm-base-url` |
| --- | --- |
| OpenCode Zen *(default)* | `https://opencode.ai/zen/v1` |
| OpenAI | `https://api.openai.com/v1` |
| Google Gemini | `https://generativelanguage.googleapis.com/v1beta/openai` |
| Mistral | `https://api.mistral.ai/v1` |
| DeepSeek | `https://api.deepseek.com` |
| xAI (Grok) | `https://api.x.ai/v1` |
| OpenRouter | `https://openrouter.ai/api/v1` |
| Groq | `https://api.groq.com/openai/v1` |
| Together AI | `https://api.together.ai/v1` |
| Fireworks AI | `https://api.fireworks.ai/inference/v1` |
| Cerebras | `https://api.cerebras.ai/v1` |
| SambaNova | `https://api.sambanova.ai/v1` |
| DeepInfra | `https://api.deepinfra.com/v1/openai` |
| Hugging Face | `https://router.huggingface.co/v1` |
| Perplexity | `https://api.perplexity.ai` |
| Scaleway | `https://api.scaleway.ai/v1` |
| Ollama *(local)* | `http://localhost:11434/v1` |
| vLLM, LM Studio, llama.cpp, LiteLLM, LocalAI | see [Providers](docs/providers.md#self-hosted-and-local) |

[docs/providers.md](docs/providers.md) has the full list, the self-hosted servers,
what is **not** compatible and why, and the one gotcha that affects every
provider.

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
| [Providers](docs/providers.md) | Which providers work, and how to configure each. |
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
