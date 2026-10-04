# Anubis examples

Copy-paste ready templates for reviewing pull requests with Anubis.

## Minimal workflow

The shortest useful setup. Anubis needs no repository checkout.

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
          anubis-llm-api-key: ${{ secrets.ANUBIS_LLM_API_KEY }}
```

1. Copy this into `.github/workflows/anubis.yml`.
2. Add an `ANUBIS_LLM_API_KEY` repository secret. Any OpenAI-compatible provider
   key works — see [Providers](../docs/providers.md) for the full list.
3. Open a pull request.

## `review.sh` — local CLI

Runs the same review locally, which is the fastest way to try a different model
or provider before wiring up CI.

```sh
# From anywhere in the repo:
./examples/review.sh owner/project 42
./examples/review.sh owner/project 42 publish   # also post the PR comment
```

Requires `GITHUB_TOKEN` and a model key in the environment:

```sh
export GITHUB_TOKEN=...
export ANUBIS_LLM_API_KEY=...

# Or point it at a local model:
MODEL=qwen3-coder LLM_BASE_URL=http://localhost:11434/v1 ./examples/review.sh owner/project 42
```

## Going further

- **Different model or provider** — set the `model` and `llm-base-url` inputs on
  the action step, or the `MODEL` and `LLM_BASE_URL` variables for `review.sh`.
- **GitHub Enterprise** — set the `github-base-url` input to your API root, for
  example `https://github.example.com/api/v3`.
- **Do not publish** — set `publish: false` to keep the review in the job log
  only, leaving the pull request untouched.

## Caveats

- On `pull_request` events from forks, repository secrets are not exposed, so
  the model key is empty and the review fails. Use `pull_request_target` only if
  you understand that it gives fork pull requests write access to your
  repository.
- The diff is sent to whatever `llm-base-url` points at. Use a self-hosted
  endpoint if the change must not leave your infrastructure.
