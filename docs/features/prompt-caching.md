# Prompt caching

Kit asks supported providers to cache each conversation's prompt so that later
requests read the unchanged prefix (tools, system prompt, and earlier messages)
instead of processing it again. Cache reads cost a fraction of the input price;
writing an entry can cost more than ordinary input. The controls and prices
depend on the endpoint and model. Kit does not treat a catalog cache-write
price as proof that a request field is supported.

## Anthropic

### How Kit caches

Every request Kit sends to Anthropic's API (`https://api.anthropic.com`),
whether authenticated with an API key or a Claude subscription, carries:

- **Automatic caching**: a request-level breakpoint that Anthropic places on the
  last cacheable block. Each request writes an entry there and reads the entry
  the previous request wrote.
- **A system-prompt breakpoint** at the end of the system prompt, so tools and
  the system prompt stay cached when the messages change, for example after
  compaction.

Kit keeps request prefixes stable within a session: the system prompt and tools
stay the same until the session's runtime is reconfigured (for example by a
reload, a model change, or a plugin or MCP tool change), and image preparation
is deterministic. Each such change starts a new cache entry.

Requests are sent without `cache_control` when they go anywhere other than
Anthropic's API:

- API-key requests to a custom `ANTHROPIC_BASE_URL`, because gateways and
  proxies may reject it. Claude subscription requests always go to Anthropic's
  API and are cached regardless.
- OpenCode Go models that use the Messages API (such as MiniMax M3), because
  not every upstream vendor behind the gateway accepts it.

### Retention

Set `promptCacheRetention` in `$KIT_HOME/settings.json`. OpenAI maps the same
setting only for the earlier models listed below; GPT-5.6 and later always use
a 30-minute `ttl`.

```json
{
  "promptCacheRetention": "long"
}
```

| Value | Anthropic lifetime | Write price |
| --- | --- | --- |
| `"short"` (default) | 5 minutes | 1.25× input |
| `"long"` | 1 hour | 2× input |

Each cache read restarts the lifetime at no extra cost. With `"short"`, a pause
of more than five minutes between requests means the next request writes the
whole prompt again; `"long"` keeps it for an hour at a higher write price.
`"long"` usually costs less when turns are often more than five minutes apart.
Any other value falls back to `"short"` with a settings warning. Kit reads the
setting for every request, so a change applies to the next request.

## OpenAI

Prompt caching is endpoint-specific. Kit sends cache controls only to the
public Responses API (`https://api.openai.com/v1`). A custom `OPENAI_BASE_URL`,
OpenCode Go, and the ChatGPT Codex endpoint keep their existing requests:
Codex and gateways are not sent `prompt_cache_options` or
`prompt_cache_retention`, because those fields are not verified there and a
gateway may reject them. Implicit provider caching can still happen, and Kit
still records cache usage when the response reports it.

Kit does not send `prompt_cache_key`, `prewarm`, or a comparison response ID.
There is no per-customer cache split, and a fresh key on every request would
hurt routing on models before GPT-5.6. Continuity does not use provider-side
transcript storage: requests keep `store: false`, include
`reasoning.encrypted_content`, and omit `previous_response_id`. Encrypted
reasoning items are replayed only for the same provider and model. A model
change drops those items and replays the assistant text, which also misses the
old model's cache prefix.

### GPT-5.6 and later

Reviewed public models whose IDs are `gpt-5.6`, `gpt-6`, or start with
`gpt-5.6-` or `gpt-6-` send:

```json
{ "prompt_cache_options": { "mode": "implicit", "ttl": "30m" } }
```

`30m` is the only documented lifetime. `promptCacheRetention` does not change
it, and Kit does not also send the deprecated `prompt_cache_retention` field.
The backend may retain an entry longer. A cache read refreshes the lifetime
without another write charge.

Implicit mode places a breakpoint at the end of the latest eligible message, so
a growing transcript can be reused. Kit also places one explicit breakpoint at
the end of the stable system prompt. Top-level `instructions` cannot carry that
breakpoint, so the system prompt is sent as the first developer input item
instead of being duplicated in `instructions`. Content after the breakpoints is
unchanged. Other endpoints still use `instructions`.

Cache writes are billed at the model's catalog cache-write price, which is
1.25× uncached input for these models. That price is not Anthropic's one-hour
write price.

### Earlier models

Only these reviewed IDs receive `prompt_cache_retention`. Prefixes and catalog
metadata do not add siblings such as `gpt-5-mini` or `gpt-4.1-mini`.

| Models | `"short"` | `"long"` |
| --- | --- | --- |
| `gpt-5.4`, `gpt-5.2`, `gpt-5.1`, `gpt-5.1-codex`, `gpt-5.1-codex-mini`, `gpt-5.1-codex-max`, `gpt-5.1-chat-latest`, `gpt-5`, `gpt-5-codex`, `gpt-4.1` | `in_memory` | `24h` |
| `gpt-5.5`, `gpt-5.5-pro` | omitted (only `24h` is supported) | `24h` |

These models have no extra cache-write charge. `in_memory` is typically 5 to
10 minutes of inactivity, up to an hour. `24h` is typically about 30 minutes
and can last up to 24 hours. Kit does not add explicit breakpoints; they are
not supported.

## Usage and cost

Session usage reports cache reads and writes separately from uncached input.
`Total` includes cached tokens. For OpenAI Responses, `cached_tokens` and
`cache_write_tokens` are a breakdown of `input_tokens`, so Kit subtracts both
before pricing input. A breakdown larger than `input_tokens` is invalid usage
and is not billed. Cost prices cache writes at the model's cache-write price.
Anthropic one-hour writes, recorded only as `CacheWrite1h`, cost twice the
input price. Clients see total cache writes; the five-minute and one-hour
split is reflected only in cost.
