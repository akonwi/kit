# Model catalog

The server's model catalog lists the models its clients offer in model pickers,
for new sessions, and in settings. OpenAI, Anthropic, OpenCode Zen, and OpenCode
Go models come from [models.dev](https://models.dev/api.json). OpenAI Codex
models are built in and don't change between releases.

## Where the catalog comes from

When the server starts, each models.dev provider lists the models of the
catalog the server last fetched, which is cached in
`$KIT_HOME/.cache/models.dev.json`. A provider the cache doesn't cover,
or a server with no cache, lists the catalog embedded in the Kit release.

The server then refreshes the catalog in the background. It refreshes once at
startup and again whenever the catalog it holds is a day old. It checks hourly,
by wall-clock time, so a computer that sleeps still refreshes about once a day.
Each refresh may take up to 30 seconds. Startup, attaching, and prompts never
wait for it.

A refresh that fails keeps the catalog the server already has and logs a
warning. This covers being offline, a timeout, an HTTP error, or a payload
that isn't a valid catalog. A failed refresh is retried at the next hourly
check. A successful refresh replaces the cache.

**Refresh models**, in the TUI's and the macOS app's command palettes,
refreshes the catalog right away. It reports the number of models listed, or
the error. It can run at the same time as a background refresh. When two
refreshes overlap, the catalog fetched by the one that started last wins.

Only the long-lived server refreshes: `kit __server`, which `kit` starts as a
daemon, and `kit server run`. Print mode and one-shot commands use that server
and never fetch the catalog themselves.

## What a refresh changes

After a refresh, each models.dev provider lists exactly the complete models
the new catalog lists for it. Models the catalog adds appear, changed metadata
such as names and limits updates, and models it no longer lists leave the
pickers. If the catalog lists no usable models for a provider, that provider
keeps its current models, so a partial or empty catalog can't empty a
provider.

A model that leaves the listing still works wherever it's already in use.
Sessions, subagents, subagent definitions, and a compaction model that use a
retired model keep resolving and streaming it under its `provider/model-id`
selector, and can still change its thinking level. A model the release embeds
stays usable this way until the next release. A model added by an earlier
refresh stays usable until the server restarts.

New choices accept only listed models: new sessions, switching a session's or
subagent's model, the default model, and the compaction and model-override
settings. A default model that is no longer listed falls back as described in
[server settings](server-settings.md#new-sessions).

A runtime that is already open keeps the model metadata it resolved. Refreshed
limits apply when a runtime next opens.

## How clients see a refresh

Clients read the catalog each time they need it, and no event announces a
refresh. The TUI's model, thinking, and settings pickers read it whenever they
open. In the macOS app, the composer's and the Subagents pane's model pickers
re-read it when they open, and the new-session sheet and server settings read
it when they appear. A picker that is already open shows the catalog it read.
