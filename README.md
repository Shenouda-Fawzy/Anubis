# Anubis

[![CI](https://github.com/Shenouda-Fawzy/Anubis/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Shenouda-Fawzy/Anubis/actions/workflows/ci.yml)
[![action version](https://img.shields.io/github/v/release/Shenouda-Fawzy/Anubis?label=action%20v1)](https://github.com/Shenouda-Fawzy/Anubis/releases/latest)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Anubis is a Github action (_and cli_) you can use it to review you and your team's PR. It ships with
four built-in reviewer specialists plus a master reviewer to ensure code quality, security, performance
and maintainability.

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
          anubis-llm-base-url: <URL> # like: https://generativelanguage.googleapis.com/v1beta/openai
          anubis-llm-model: <preferred-llm-model> # like: gemini-3.6-flash
```

You only need to add **one** secret, `ANUBIS_LLM_API_KEY`, holding your model provider's key
   (Settings → Secrets and variables → Actions).

**You don't like the built-in specialists?** No problem: create a `.anubis-agents` directory at the
root of your repo and define your own agent specialists (each a `*.md` file with optional front
matter for `name`, `description` and `model`). One special file, `master-agent.md`, defines the
master agent that runs after all the specialists to help deduplicate the findings. Turn the feature
on with the `anubis-agents: 'true'`.
See [`docs/behavior.md`](docs/behavior.md#custom-agents) for the full behavior.

See [`examples/vuln-hunter.md`](examples/vuln-hunter.md) for an example specialist agent, and
[`examples/anubis.yml`](examples/anubis.yml) for the complete configuration.


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
`maintainability` — review the diff one at a time, each with its own review
prompt and focus. A coordinator then discards false positives, deduplicates by
root cause, and re-grades severity. The result is posted as a comment.

A repository can replace the panel with its own reviewers: put `*.md` files in
`.anubis-agents/` at the repo root (see
[Custom agents](docs/behavior.md#custom-agents)) and set `anubis-agents: 'true'`.
Agents load from the pull request's own branch, so they work on a feature branch
before they are merged; set `anubis-allowed-target-branch` to restrict them to
pull requests targeting one branch. It is a hard requirement — if the agents
cannot be loaded, the run fails rather than fall back to the built-ins.

## Providers

Anubis speaks one protocol — OpenAI's `POST {base}/chat/completions` — so switching
provider is a URL and a key. No adapter, no code change:

```yaml
- uses: Shenouda-Fawzy/Anubis@v1
  with:
    anubis-llm-api-key: ${{ secrets.ANUBIS_LLM_API_KEY }}
    anubis-llm-base-url: https://api.deepseek.com   # any OpenAI-compatible endpoint
    anubis-llm-model: deepseek-chat
```

| Provider | `anubis-llm-base-url` |
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
| Perplexity | `https://api.perplexity.ai/router/v1` |
| Scaleway | `https://api.scaleway.ai/v1` |
| Ollama *(local)* | `http://localhost:11434/v1` |
| vLLM, LM Studio, llama.cpp, LiteLLM, LocalAI | see [Providers](docs/providers.md#self-hosted-and-local) |

[docs/providers.md](docs/providers.md) has the full list, the self-hosted servers,
what is **not** compatible and why, and the one gotcha that affects every
provider.

## Common changes

| To do this | Set this |
| --- | --- |
| Use another model or provider | `anubis-llm-model`, `anubis-llm-base-url` |
| Run the reviewers in parallel | `anubis-max-concurrency` |
| Keep the review out of the PR | `publish: false` |
| Define your own review agents | `anubis-agents: 'true'` + `.anubis-agents/*.md` |
| Restrict custom agents to PRs targeting one branch | `anubis-allowed-target-branch: main` |
| See more detail in the log | `anubis-log-level: debug` |

Every input, flag and environment variable is in
[docs/configuration.md](docs/configuration.md).

## License

[MIT](LICENSE)
