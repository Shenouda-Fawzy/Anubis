#!/usr/bin/env sh
# Local Anubis review using custom Markdown agents from a directory.
#
# Usage: ./review.sh owner/project PR [publish]
#   owner/project - repository to review (e.g. Shenouda-Fawzy/Anubis)
#   PR            - pull request number
#   publish       - pass "publish" to post the review as a PR comment
#
# Requires GITHUB_TOKEN and a model API key in the environment
# (OPENCODE_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY).
set -e

REPO="${1:?usage: review.sh owner/project PR [publish]}"
PR="${2:?usage: review.sh owner/project PR [publish]}"
PUBLISH="${3:-}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

go run "$ROOT/cmd/anubis" \
  -repo "$REPO" \
  -pr "$PR" \
  -agents "$ROOT/examples/agents" \
  ${PUBLISH:+-publish}