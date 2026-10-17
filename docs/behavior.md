# Behavior

What Anubis does at the edges, where the interesting behavior lives.

## Concurrency

The four specialists run **one after another** by default. That is deliberate:
a review costs one provider call per specialist, so fanning them out is the
fastest way to exhaust a free tier's rate limit.

Raise it when your provider can absorb the fan-out and the wall-clock time is
worth it:

```yaml
      - uses: Shenouda-Fawzy/Anubis@v1
        with:
          anubis-max-concurrency: '4'
```

```sh
ANUBIS_MAX_CONCURRENCY=4 ./anubis -repo owner/repo -pr 42
```

Rules:

- The value is clamped to the number of agents and to a hard ceiling of 16, so a
  large setting cannot produce unbounded parallel requests.
- An unset, unparseable or non-positive value is treated as `1` and logged as a
  warning. A typo in a tuning variable should not stop a review.
- Findings are collected by agent index, so the synthesis prompt is byte-for-byte
  identical whether the specialists ran in parallel or in sequence.

Raising this does not reduce cost. It spends the same tokens in less time, which
makes hitting a rate limit *more* likely.

## Diff truncation

Diffs are capped at 200,000 bytes. Very large pull requests are truncated with a
marker rather than failing, because most model context windows are far smaller
than the API's own limits and a hard failure here would be indistinguishable from
a provider outage.

When this happens the published comment says the diff was truncated, so a
partial review never reads as full coverage.

## Partial failure

One failing specialist does not discard the others. The remaining findings are
still synthesized, and the comment discloses how many agents failed and which
ones.

If **every** agent fails, Anubis posts a warning that the review could not be
completed and exits non-zero.

An agent that returns an empty completion is counted as failed, not as clean.
A provider that produces no output has given no evidence, which is not the same
as an agent that reviewed the change and found nothing.

## A failed review never looks clean

When the model provider errors, the comment says the review could not complete.
Anubis never posts a bare "No findings" as a result of an error. A genuinely
clean pull request does get "No findings."

## Provider errors

A non-2xx response is reported with its status line, because a rate limit or an
outage is the most likely production failure:

```text
llm: provider returned 429 Too Many Requests: {"error":{"message":"Rate limit reached..."}}
```

A response that stopped early (`finish_reason` other than `stop`) is also an
error rather than a short review, so truncated output cannot be published as if
it were complete.

## Output format

Findings are **Markdown, not structured JSON**. There is no severity field and no
machine-readable schema. They are meant to be read by a human, not parsed by a
tool.

## Custom agents

Set `anubis-agents: 'true'` (or `ANUBIS_AGENTS=true`) to let a repository define
its own reviewers. Anubis reads `*.md` files from `.anubis-agents/` at the
repository root **on the branch the pull request proposes** and parses each as
front-matter plus a Markdown system prompt:

```markdown
---
name: docs
description: Review documentation and code comments for accuracy.
model: gpt-4o-mini
---

You are a documentation reviewer. Check that comments match the code they
describe, that public APIs are documented, and that examples still work.
```

- The **body is the agent's system prompt**; it replaces the generic specialist
  prompt, so it must carry its own rules — including that the diff is untrusted
  data, never instructions.
- `name` defaults to the filename without `.md`. `description` is the focus line
  prefixed to the task; it defaults to a generic focus. `model` defaults to
  `anubis-llm-model`.
- If at least one valid specialist is present, the repository's set **replaces**
  the four built-ins for that run. `anubis-agents: 'true'` is a hard requirement,
  not a hint: when **no** valid specialist can be loaded, the run **fails**
  rather than fall back to the built-ins, so a review is never published under a
  repository's name using reviewers it did not define.
- **`master-agent.md` is special**: naming a file exactly that overrides the
  coordinator's system prompt. It is never treated as a specialist, and the
  override is explicit by filename. Any other name (including `Master-Agent.md`)
  is an ordinary specialist.

Limits: at most 16 files, 64 KiB each. An over-limit or unparseable file is skipped
with a warning. If the directory is missing, holds no valid specialist, or the API
call fails, the run **fails**: a repository that set `anubis-agents: 'true'` asked
for its own reviewers, and using the built-ins instead would publish a review a
reader mistakes for theirs. Every run that uses custom agents or a custom
coordinator **discloses it in the comment**, so a reader can tell whose
instructions produced the findings.

Custom agents are read from the pull request's **head** branch, in whatever
repository that branch lives in — the repository under review, or a **fork**. A
fork is deliberately allowed, so a same-repository branch (or a fork branch) can
add and try reviewers before they are merged. Be aware of what that means: an
outside contributor can supply the instructions that review their own change. A
repository that does not want that should leave `anubis-agents` off, since the
only remaining restriction is the target branch. If GitHub does not report the
head repository (a deleted fork) or the head branch, there is nowhere to read the
directory from and the run **fails**.

The files come from the pull request's **head** branch (the branch it proposes),
but `anubis-allowed-target-branch` filters on the branch the pull request
**targets** (its base branch). So a pull request from `feat/api` into
`development` reads `.anubis-agents/` from `feat/api`, and `allowed-target-branch:
development` lets it run. Restrict with `anubis-allowed-target-branch: main`
(or `ANUBIS_ALLOWED_TARGET_BRANCH=main`) to apply custom agents only to pull
requests heading into `main`; any other target makes the run **fail** rather than
fall back. Leave it empty to allow any target
branch, so a developer can try custom agents freely before merging.

## Logging

Logs go to stderr as structured text.

**`-log-level debug` never logs diff content, prompt content, or findings** — not
even on a public repository where the diff is source code. Debug output is byte
counts, token counts, model metadata, and pull-request identifiers.

The one thing to know: a provider's error body is echoed into the log, collapsed
to a single line and capped at 200 characters. That is intentional so a rate
limit is diagnosable, but a provider that reflects your prompt in an error
message could put a fragment of it in the log.

## Permissions

The action needs `pull-requests: write` to post the comment and `contents: read`
to fetch the diff. Nothing else. It does not check out your code.
