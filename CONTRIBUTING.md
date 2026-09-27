# Contributing to Anubis

Thanks for helping. This is a small project and review is the bottleneck, so
keeping changes focused and well tested matters more than volume.

## Getting set up

Requires Go 1.26.x.

```sh
git clone https://github.com/Shenouda-Fawzy/Anubis
cd Anubis
make test        # should pass with no setup, network or secrets
```

The suite is fully hermetic. It builds the real binary and drives it against
local `httptest` mock servers for both the GitHub API and the model provider.
If a test needs the network or a real credential, it is in the wrong place.

## Before you open a PR

CI must be green. It runs:

| Check | Locally |
| --- | --- |
| `gofmt -l cmd` reports nothing | `make fmt-check` |
| `go vet` | `make vet` |
| `go test -race` | `make test` |
| `golangci-lint` | `make lint` |
| `docker build .` | `docker build -t anubis .` |

That last one matters more than usual: GitHub builds the runtime image from the
`Dockerfile` at run time, so a broken build breaks every consumer of the action.

Please also add a test. A new behavior without a test is not finished. Run
`go test -race ./...` if you touch anything concurrent.

## Changing the review pipeline

The prompts in `masterprompt.go` and `subagentprompt.go` are the product. A
prompt edit changes every review for every user, so:

- Keep the coordinator and the specialists distinct. Specialists gather
  evidence; the coordinator judges it. Do not make specialists arbitrate, and do
  not make the coordinator re-review the whole diff.
- Agents must receive the diff. If you add an agent, render its task with
  `subAgentTask` so the PR context and diff come along.
- Findings stay free-form Markdown. There is no JSON schema, no severity field
  and no `Approved` flag in the code; the severity levels exist only as prompt
  instructions. Do not code around that assumption.

## Things that will not be accepted

- **Logging diff, prompt or finding content.** On a public repository the diff
  is source code, and findings can echo secrets found in it. Log sizes, counts
  and metadata only. This is checked in review because it is a real leak.
- **A new dependency** without a reason worth stating in the PR. There is one
  today, `go-github`.
- **A failing review that looks clean.** If the model errors, the comment must
  say the review could not complete. Never post "No findings" as a result of an
  error, and never silently drop failed agents.
- **Reintroducing Markdown-defined agents** without a design discussion. That
  approach was dropped before the first release.

## Commit messages and PRs

Conventional-ish, one logical change per commit, imperative subject line:

```text
fix: use the documented OPENCODE_API_KEY variable
fix(coordinator): pass the PR number to the synthesis prompt
test: cover the API key fallback order
docs: correct the action input table
```

In the PR description, state what changes for a user, not just what changed in
the code. If you touch a prompt, say what behavior you expect to change.

## Reporting bugs

Open an issue with the Anubis version, the Go version, what you expected, and
what happened. If the bug needs a real pull request, a minimal `pull_request`
diff that triggers it is worth a thousand words.

Security issues go through [SECURITY.md](SECURITY.md), not the issue tracker.
