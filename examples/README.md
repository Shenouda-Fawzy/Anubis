# Anubis examples

Ready-to-use templates for reviewing pull requests with Anubis.

## `anubis-custom-agents.yml` — GitHub Action with custom agents

Loads review agents from a directory committed to your repository instead of
using the built-in agents.

1. Copy this file to `.github/workflows/anubis.yml` in your repo.
2. Copy the sample agents to the location referenced by `agents-dir`:

   ```sh
   cp -r agents .github/anubis/agents
   ```

   or change `agents-dir` in the workflow to `examples/agents` if you keep
   Anubis's `examples/` tree.
3. Add an `OPENCODE_API_KEY` secret (or any OpenAI-compatible / Gemini key).
4. Open a pull request — Anubis posts its review as a comment.

Notes:

- `actions/checkout@v4` is required only because the action reads the agent
  files from the workspace.
- On `pull_request` events from forks, repo secrets are not exposed; use
  `pull_request_target` only if you understand the security implications.
- The built-in agents need no checkout and no `agents-dir` — omit both (see
  the quick start in the root `README.md` for the minimal workflow).

## `agents/` — sample Markdown agents

The three files here mirror the built-in subjects (correctness, maintainability,
security) and show the YAML front matter format. Each file may override
`model`, `max_tokens`, `temperature`, and `enabled` per agent. See the
"Markdown agents" section of the root `README.md`.

## `review.sh` — local CLI

Runs Anubis locally against the same custom agents, useful for testing before
wiring up CI:

```sh
# From anywhere in the repo:
./examples/review.sh owner/project 42
./examples/review.sh owner/project 42 publish   # also post the PR comment
```

Requires `GITHUB_TOKEN` and a model key (`OPENCODE_API_KEY`, `OPENAI_API_KEY`,
or `GEMINI_API_KEY`) in the environment. Uses OpenCode Zen's free
`big-pickle` model by default — see the "Other providers" section of the root
`README.md` for flags such as `-model` and `-llm-base-url`.