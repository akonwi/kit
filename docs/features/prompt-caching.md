# Prompt caching

Kit asks Anthropic to cache each conversation's prompt so that later requests
read the unchanged prefix (tools, system prompt, and earlier messages) instead
of processing it again. Cache reads cost a tenth of the input price or less;
writing an entry costs more than ordinary input.

## How Kit caches

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

## Retention

Set `promptCacheRetention` in `$KIT_HOME/settings.json`:

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

## Usage and cost

Session usage reports cache reads and writes separately from uncached input.
`Total` includes cached tokens. Cost prices cache writes at the model's
cache-write price and one-hour writes at twice the input price. Clients see
total cache writes; the five-minute and one-hour split is reflected only in
cost.
