# Providers

Anubis talks to one protocol: OpenAI's `POST {base}/chat/completions`. Any
endpoint that speaks it works, and the key is sent as a `Bearer` token. There is
no Anubis-specific integration, adapter or SDK per provider.

- **Which providers work:** the table below, with the base URL to configure.
- **What "works" means precisely:** the [protocol section](#the-protocol-in-detail).
- **What to watch out for:** [gotchas](#gotchas-that-bite-every-provider).

## Supported providers

Set `llm-base-url` to the base URL and `anubis-llm-api-key` to that provider's key.
Nothing else changes.

Every row below was checked against that provider's own documentation rather than
assumed from the "OpenAI-compatible" label. Where a provider's documentation did
**not** settle a question, the row says so instead of guessing.

### Hosted APIs

| Provider | `llm-base-url` | Notes |
| --- | --- | --- |
| **OpenCode Zen** (default) | `https://opencode.ai/zen/v1` | Free `big-pickle`. See [model routing](#opencode-zen-model-routing) before changing `model`. |
| **OpenAI** | `https://api.openai.com/v1` | The reference implementation. |
| **Google Gemini** | `https://generativelanguage.googleapis.com/v1beta/openai` | Google's OpenAI-compatibility layer. Path is `v1beta`, and any parameter Anubis does not send is silently ignored, so thinking and safety settings stay at their defaults. |
| **Mistral** | `https://api.mistral.ai/v1` | Only `model` and `messages` are required. |
| **DeepSeek** | `https://api.deepseek.com` | **No `/v1`** — the documented base URL has no version segment. Reasoning is on by default; see [gotchas](#gotchas-that-bite-every-provider). |
| **xAI (Grok)** | `https://api.x.ai/v1` | Works today. xAI labels chat completions a legacy endpoint and is steering new features to its Responses API. |
| **OpenRouter** | `https://openrouter.ai/api/v1` | One key, many models. `HTTP-Referer` and `X-Title` are optional and Anubis does not send them. |
| **Groq** | `https://api.groq.com/openai/v1` | Note the `/openai` path segment. |
| **Together AI** | `https://api.together.ai/v1` | |
| **Fireworks AI** | `https://api.fireworks.ai/inference/v1` | |
| **Cerebras** | `https://api.cerebras.ai/v1` | |
| **SambaNova** | `https://api.sambanova.ai/v1` | |
| **DeepInfra** | `https://api.deepinfra.com/v1/openai` | Note the `/openai` path segment. |
| **Hugging Face** | `https://router.huggingface.co/v1` | Inference Providers router. The key is a Hugging Face access token. |
| **Perplexity** | `https://api.perplexity.ai` | **No `/v1`.** Only the Sonar chat models are chat-completions compatible. |
| **Scaleway** | `https://api.scaleway.ai/v1` | Managed Generative APIs. Dedicated deployments use a per-deployment host instead. |
| **Cloudflare Workers AI** | `https://api.cloudflare.com/client/v4/accounts/<account-id>/ai/v1` | Your account ID is part of the path, so this one cannot be a fixed constant. |

### Self-hosted and local

| Server | `llm-base-url` | Key |
| --- | --- | --- |
| **Ollama** | `http://localhost:11434/v1` | Ignored, but **must be non-empty** — see below. |
| **LM Studio** | `http://localhost:1234/v1` | Ignored unless you enable auth in the server. |
| **llama.cpp** | `http://localhost:8080/v1` | Optional, via `--api-key`. |
| **vLLM** | `http://localhost:8000/v1` | Optional, via `--api-key`. |
| **LiteLLM** | `http://localhost:4000` | Required. |
| **LocalAI** | `http://localhost:8080/v1` | Optional. Note it shares port 8080 with llama.cpp. |
| **text-generation-webui** | `http://127.0.0.1:5000/v1` | Optional. Ignores `model` — load the model in the UI first. |

**A local server that ignores the key still needs one.** Anubis refuses to make a
call with an empty credential, because every hosted provider needs a real key and
a silent request without one is far harder to debug than a clear error. Pass any
placeholder:

```yaml
with:
  anubis-llm-api-key: ollama
  llm-base-url: http://localhost:11434/v1
  model: qwen3-coder
```

Most of these servers document the endpoint and the auth header, but only some
publish the **response** shape. LiteLLM and text-generation-webui document
`finish_reason` explicitly; for Ollama, LM Studio, llama.cpp, vLLM and LocalAI it
is implied by the compatibility claim rather than stated. They work in practice,
but that part is convention, not spec.

## Not compatible

| Provider | Why |
| --- | --- |
| **Anthropic** (native Messages API) | Different protocol: `x-api-key`, mandatory `anthropic-version`, mandatory `max_tokens`. Anubis can send none of those. |
| **Anthropic** (OpenAI-compatibility layer) | It does expose `/v1/` in OpenAI shape, so it is technically reachable — but Anthropic's own docs say the layer is "primarily intended to test and compare model capabilities, and is not considered a long-term or production-ready solution for most use cases", the default output cap on that layer is undocumented, and a key spanning multiple workspaces also needs an `anthropic-workspace-id` header Anubis cannot send. Do not build on it. |
| **Azure OpenAI**, classic endpoint | `POST /openai/deployments/<name>/chat/completions?api-version=...` needs a query parameter and a deployment in the path. Anubis can append a path but cannot add a query string. Use the v1 surface below instead. |
| **GitHub Models** | Retired on 30 July 2026. The inference API and catalog no longer exist. |

### Azure OpenAI, the v1 surface

```text
https://<resource>.openai.azure.com/openai/v1
```

Works, with two things to get right:

- **`model` is your deployment name**, not the model name. Sending the model name
  returns a 404 that does not explain itself.
- Auth is `Authorization: Bearer`, which the endpoint's spec lists as valid.
  Azure's own examples use an `api-key` header instead, which Anubis cannot send,
  so Bearer is the only option that can work here.

## The protocol in detail

What Anubis actually sends. A provider must satisfy all of it to be drop-in:

```http
POST {base}/chat/completions
Authorization: Bearer <your key>
Content-Type: application/json

{"model": "<id>", "messages": [{"role": "system", ...}, {"role": "user", ...}]}
```

That is the entire request body — no `temperature`, no `max_tokens`, no
`stream`, no `response_format`, no tools. And the response must be ordinary
chat-completions JSON, because Anubis reads:

| Field | Requirement |
| --- | --- |
| `choices[0].message.content` | Present, and a plain JSON string. |
| `choices[0].finish_reason` | Exactly `"stop"`. |
| `usage.prompt_tokens` etc. | Optional; only reported in logs. |

## Gotchas that bite every provider

**`finish_reason` must be `"stop"`, or the review fails.** Anubis discards a
response that stopped for any other reason, because content that was cut off is
not a review, and a truncated review that reads as clean is worse than a failure.
The usual cause is `length`: the model ran out of output room or context window.

Because Anubis sends no `max_tokens`, there is no output cap to tune and
truncation comes from the **context window** instead. On a hosted provider with a
large window this is rare. On a local model it is the default outcome unless you
raise the window:

| Server | How to raise it |
| --- | --- |
| Ollama | `PARAMETER num_ctx` in a Modelfile. Ollama has no API parameter for this. |
| llama.cpp | `-c <tokens>` |
| vLLM | `--max-model-len` |
| LM Studio | The context length loaded with the model |

A second, subtler risk: some providers return `finish_reason` values Anubis does
not expect on a normal completion. DeepSeek can return `length`,
`content_filter`, `insufficient_system_resource` or `aborted`; a safety filter
returning `content_filter` on a security review is plausible, since that is
exactly the kind of prompt that trips one.

## Building the URL

`llm-base-url` is used as-is unless it already ends in `/chat/completions`, in
which case nothing is appended. All of these are equivalent:

```text
https://opencode.ai/zen/v1                  -> .../v1/chat/completions
https://opencode.ai/zen/v1/                 -> .../v1/chat/completions
https://opencode.ai/zen/v1/chat/completions -> used as-is
```

Trailing slashes are fine. When in doubt, copy the base URL from the provider's
OpenAI-compatibility page verbatim, including any path segment: DeepInfra,
Groq and Perplexity all need one that a guess would miss.

## OpenCode Zen model routing

Zen routes **per model**, so a model being listed in Zen is not enough. Check the
model you want against
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