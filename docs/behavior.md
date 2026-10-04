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

- The value is clamped to the number of agents and to a hard ceiling of 4, so a
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
