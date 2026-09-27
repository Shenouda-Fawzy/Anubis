# Providers

Anubis talks to one protocol: OpenAI's `POST {base}/chat/completions`. Any
endpoint that speaks it works, and the key is sent as a `Bearer` token.

## Building the URL

`llm-base-url` is used as-is unless it already ends in `/chat/completions`, in
which case nothing is appended. All of these are equivalent:

```text
https://opencode.ai/zen/v1                  -> .../v1/chat/completions
https://opencode.ai/zen/v1/                 -> .../v1/chat/completions
https://opencode.ai/zen/v1/chat/completions -> used as-is
```

## Which models work

Anubis sends and expects the chat-completions request and response shape. A
model is supported when the endpoint you configure serves it over that shape.

### OpenCode Zen

Zen routes **per model**, so a model being listed in Zen is not enough. Check
the model you want against
[OpenCode's endpoint table](https://opencode.ai/docs/zen/#endpoints):

| Model family on Zen | Endpoint | Works with Anubis |
| --- | --- | --- |
| `big-pickle`, `qwen3.8-max`, `deepseek-v4*`, `glm-*`, `kimi-*`, `minimax-*` | `/zen/v1/chat/completions` | Yes |
| `gpt-*`, `grok-*`, `muse-spark-*` | `/zen/v1/responses` | No |
| `claude-*`, `qwen3.*-flash/plus` | `/zen/v1/messages` | No |
| `gemini-*` | `/zen/v1/models/<id>` | No |

Selecting a model from a row marked **No** fails, because the request goes to
`/chat/completions` and that model is not served there. The failure names the
HTTP status, so the cause is visible in the log.

### Other providers

```sh
# Google Gemini
export OPENCODE_API_KEY=<gemini-api-key>
./anubis -repo owner/repo -pr 42 \
  -llm-base-url https://generativelanguage.googleapis.com/v1beta/openai \
  -model gemini-2.5-flash

# Local Ollama
./anubis -repo owner/repo -pr 42 \
  -llm-base-url http://localhost:11434/v1 -model qwen3-coder
```

Both work through the same code path as Zen. There is nothing Anubis-specific
about them.

## Cost

A review is one call per specialist plus one synthesis call: **five calls**.

`big-pickle` is currently free on Zen. Zen also **auto-reloads $20 when a
balance drops below $5**, and that is on by default. Free models will not
trigger it, but if you switch `model` to a paid one, confirm your Zen auto-reload
setting first.

## Where your diff goes

By default Anubis sends the pull-request title, description and **the entire
diff** to OpenCode Zen's `big-pickle` model at `https://opencode.ai/zen/v1`.
That is your source code leaving your infrastructure.

**The default model is not zero-retention.** OpenCode's privacy policy states
that during the model's free period, submitted data *may be used to improve the
model*. `big-pickle` is a free "stealth" model, so out of the box your diffs can
be used for model improvement. Do not point the default at a repository with
code you would not publish.

To keep the diff inside your own infrastructure, set `llm-base-url` to a
self-hosted OpenAI-compatible endpoint such as Ollama or vLLM, and verify that
provider's terms yourself. Anubis makes no claims about any provider's data
handling beyond what that provider publishes.

This is separate from the model output itself, which is also untrusted. See
[SECURITY.md](../SECURITY.md) for the full threat model.
