# Security Policy

## Supported versions

| Version | Supported |
| --- | --- |
| `v1` (latest tag) | Yes |
| `main` | Yes |
| Anything older | No |

Security fixes land on `main` and in the next tag. There is no long-term
support branch.

## Reporting a vulnerability

Please do **not** open a public issue for a security problem.

Use GitHub's private reporting via the "Security" tab on this repository
("Report a vulnerability"). That is the only reporting channel for this project.

Please include:

- What the issue is, and which file or workflow is involved.
- How to reproduce it, ideally a minimal `pull_request` or a CLI invocation.
- What an attacker gains, and what they need in order to do it.
- Whether the pull-request diff, comment content, or the model key is exposed.

You can expect an acknowledgement within a week. Fixes are released as a patch
tag, and the advisory is published at the same time. Credit is given in the
advisory unless you prefer otherwise.

## Where your diff goes

By default Anubis uploads the pull-request title, description and **the entire
diff** to OpenCode Zen's `big-pickle` model. That is your source code leaving
your infrastructure.

**The default model is not zero-retention.** OpenCode's privacy policy states
that during the model's free period, submitted data *may be used to improve the
model*. Do not point the default at a repository with code you would not
publish. Set `anubis-llm-base-url` to a self-hosted endpoint to keep the diff
inside your own infrastructure.

See [Where your diff goes](docs/providers.md#where-your-diff-goes) for the
details and the mitigation.

## Threat model

Anubis runs an AI model over **untrusted input** — the pull request title,
description and diff are all supplied by whoever opened the pull request. It is
also a GitHub Action that holds a token with `pull-requests: write`.

What Anubis does:

- Instructs every reviewer, and the coordinator, to treat the diff as data to
  analyze and never as instructions to follow. This is the primary defense
  against prompt injection through a malicious diff.
- Never logs diff content, prompt content, or findings — even at
  `-log-level debug`. Only byte counts, token counts, model metadata, and PR
  identifiers.
- Writes only to the pull request it was asked to review.
- Builds a fully static binary and runs in a distroless `nonroot` image.

What Anubis does **not** do, and does not claim to:

- **The model output is not verified.** A review is generated text, not a
  guarantee. Prompt injection through a diff is mitigated, not eliminated. Read
  the comment before acting on it, and do not let it trigger deployments.
- **The diff is sent to the configured endpoint.** See
  [Where your diff goes](#where-your-diff-goes) above — the default model is not
  zero-retention.
- **It does not sandbox the model.** There is no retry-until-safe or output
  filtering beyond the prompt instructions.
- **`pull_request_target` is dangerous by design.** Running Anubis on
  `pull_request_target` gives fork pull requests write access to your
  repository. Prefer `pull_request`, and accept that fork PRs will not have
  access to your model key.

## Verifying a release

Every action run builds the image from the `Dockerfile` in the referenced
ref. To pin a known-good build, pin a tag:

```yaml
- uses: Shenouda-Fawzy/Anubis@v1
```

CI builds and tests the image on every push, so a broken `Dockerfile` fails
before it reaches you.
