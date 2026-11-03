# 0057: Keep the model catalog fresh

## Status

Accepted.

## Context

The server's model catalog decides which models clients offer and which
selectors resolve. The models.dev providers (OpenAI, Anthropic, OpenCode Zen,
and OpenCode Go) take their models from the models.dev catalog. Each Kit
release embeds a copy, and the server caches the last catalog it fetched.
Providers add and retire models between releases, so a catalog that is fetched
only on request drifts on a server that runs for weeks. A catalog that only
ever grows keeps offering models that providers have retired.

Retiring a model can't break work that already uses it. Sessions, subagents,
and the compaction-model setting resolve their model by selector whenever a
runtime opens. A session's model can only change through its runtime, so a
selector that stopped resolving would leave its session unable to load or to
switch models.

The catalog can be unreachable: the server may be offline, models.dev may be
slow, or a response may not be a valid catalog. None of that may slow the
server's startup or its sessions.

## Decision

### The server refreshes the catalog in the background

The long-lived server refreshes the catalog when it starts and again whenever
the catalog it holds is a day old, measured in wall-clock time. That server is
`kit __server`, which clients start as a daemon, or `kit server run`. The
server compares the catalog's age hourly, which also retries a failed refresh
within an hour. Each refresh is bounded by a 30-second timeout.

One goroutine owned by the server's run does the refreshing, under a context
that server shutdown cancels. Shutdown waits for that goroutine before it tears
down the session manager, so a refresh never outlives the server. Startup,
session attach, and turns never wait for a refresh. A failed refresh keeps the
current catalog and is logged as a warning. A refresh canceled by shutdown is
not reported.

The policy, meaning when and how often to refresh, belongs to the Kit server.
The mechanism belongs to the application-neutral droids registry:
`RefreshModels`, the cache, and an optional catalog URL in `RegistryOptions`.
A manual refresh (`refreshModels`) and the background refresh use the same
mechanism. The registry fetches outside its locks and serializes commits, so
the two can overlap safely while sessions resolve models. Refreshes are
numbered as they start, and a refresh that finishes after a later-started one
has committed discards its result, so an older catalog never replaces a newer
one.

### A refresh defines the listing, and retired models stay resolvable

After a refresh, each models.dev provider lists exactly the complete models the
catalog lists for it. A cached catalog does the same when the server starts. A
catalog with no usable models for a provider leaves that provider's models as
they are, so a partial or empty response can't empty a provider.

A model that leaves the listing is retired, not removed. It no longer appears
in `Models` or `Model`, so the catalog clients read no longer shows it.
`Resolve` and `Stream` still serve it under its namespaced selector, so a
runtime that uses it keeps working. Existing configuration keeps a retired
model: a session or subagent on it still loads and can change its thinking
level, and subagent definitions and the compaction-model setting that name it
still resolve. New choices require a listed model: creating a session,
switching a session's or subagent's model, and setting the default,
compaction, or override models. A model in the embedded catalog stays
resolvable for as long as that release runs. A model only an earlier refresh
supplied stays resolvable until the server restarts.

### Clients re-read the catalog instead of being notified

No event announces a refresh, and the session protocol is unchanged. Clients
read the catalog (`listModels`) when they offer a choice of model: the TUI's
pickers every time they open, the macOS composer and Subagents model pickers
when they open, and the new-session sheet and server settings when they appear.
A refreshed catalog therefore reaches a client the next time it offers models.

## Consequences

- A server that stays up keeps a catalog at most about a day old when
  models.dev is reachable, and nobody needs to run Refresh models.
- The server makes one request to models.dev a day, plus one an hour while
  refreshes fail. Offline and air-gapped servers log a warning each hour and
  otherwise behave as before. There is no setting to turn refreshing off.
- Retired models leave pickers without breaking the sessions that use them.
  Kit doesn't yet tell a user that their session's model has been retired.
- A picker that is already open when a refresh lands keeps showing the catalog
  it read until it opens again.
- A model that was added and then retired between two releases stops
  resolving after the server restarts. A session on that model can't load
  until it can switch models without its runtime.
