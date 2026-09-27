#!/usr/bin/env sh
# Run an Anubis review locally.
#
# Usage: ./examples/review.sh owner/project PR [publish]
#   owner/project - repository to review, e.g. Shenouda-Fawzy/Anubis
#   PR            - pull request number
#   publish       - pass "publish" to also post the review as a PR comment
#
# Requires GITHUB_TOKEN and a model provider key in the environment
# (OPENCODE_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY or AI_API_KEY).
# With neither set the run uses OpenCode Zen's free big-pickle model, which
# still needs a key: OPENCODE_API_KEY.
#
# Override the provider with MODEL, LLM_BASE_URL and REPO_ROOT.
set -eu

REPO="${1:?usage: review.sh owner/project PR [publish]}"
PR="${2:?usage: review.sh owner/project PR [publish]}"
PUBLISH="${3:-}"

ROOT="${REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
MODEL="${MODEL:-big-pickle}"
LLM_BASE_URL="${LLM_BASE_URL:-https://opencode.ai/zen/v1}"

# shellcheck disable=SC2086 # PUBLISH is intentionally an optional empty word.
exec go run "$ROOT/cmd/anubis" \
  -repo "$REPO" \
  -pr "$PR" \
  -model "$MODEL" \
  -llm-base-url "$LLM_BASE_URL" \
  ${PUBLISH:+-publish}
