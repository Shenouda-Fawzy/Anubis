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
- **Configurable agent set.** The four built-in specialists are hard-coded. A
  reviewed, minimal configuration format for adding or removing specialists is
  wanted, but the earlier Markdown-with-front-matter approach was dropped
  before the first release and should not be reintroduced without a design
  discussion.
- **Cost and latency reporting.** Token counts are logged at debug level; surfacing
  them as step outputs would help consumers budget.
