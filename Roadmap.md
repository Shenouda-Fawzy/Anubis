# Roadmap

Near term, roughly in priority order:

- **Stronger prompt-injection hardening.** Reviewers are already told to treat
  the diff as untrusted data, but the mitigation is prompt-level only. Worth
  considering structural defenses: separating the diff from instructions in the
  request itself, capping how much of the diff any single instruction-shaped
  span can influence, and detecting suspicious instructions before spending
  tokens on them.
- **Per-agent models.** Sub-agents could run on a cheaper reasoning model while
  the coordinator uses a stronger one. `AgentCard.Model` already exists for this.
- **Structured findings.** Findings are currently free-form Markdown intended for
  humans. A JSON schema would let consumers gate a workflow, or write inline
  review comments, without parsing English. Needs a
  `response_format`/`json_schema` request path and a compatibility story for
  providers that do not support it.
- **Server-side pull-request review comments** instead of a single summary
  comment, so findings land inline on the diff.
- **Custom agent sets.** Shipped: a repository can replace the built-in
  specialists and the coordinator with Markdown files under `.anubis-agents/` on
  the default branch (see [Behavior](docs/behavior.md#custom-agents)). Not yet
  supported: reading the directory from a ref other than the default branch, and
  subdirectories. The trusted-ref rule (default branch, never the PR head) is
  deliberate and should not be relaxed without a security review.
- **Cost and latency reporting.** Token counts are logged at debug level; surfacing
  them as step outputs would help consumers budget.
