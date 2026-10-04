# Anubis documentation

Anubis is a static Go binary and a Docker-based GitHub Action. There is no server
and nothing to host.

| Page | Covers |
| --- | --- |
| [Configuration](configuration.md) | Every action input, CLI flag and environment variable. |
| [Providers](providers.md) | Which providers and models work, how to configure each, what a review costs, and where your diff goes. |
| [Behavior](behavior.md) | Diff truncation, partial failure, logging, and concurrency. |
| [Development](development.md) | Building, testing, and how the code is laid out. |
| [examples/anubis.yml](../examples/anubis.yml) | The complete configuration: every input, with the reasoning for each. |

Security policy and vulnerability reporting live in
[SECURITY.md](../SECURITY.md), which is kept at the repository root by
convention.

## The shape of a review

1. Fetch the pull request and its diff from the GitHub REST API.
2. Four built-in specialists — `security`, `correctness`, `performance` and
   `maintainability` — review the diff, each with the same specialist system
   prompt and a different focus. They run one at a time by default.
3. A coordinator model receives the diff plus every specialist's findings, then
   discards false positives, deduplicates by root cause, resolves
   disagreements, and re-grades severity.
4. The result is posted as a pull-request comment.

A review costs one provider call per specialist plus one synthesis call, so five
calls in total by default.
