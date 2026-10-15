# Server settings

The settings that govern what a Kit server does live in the server's
`$KIT_HOME/settings.json`, and clients read and change them through the
session API. A client attached to a server on another machine changes that
server's settings, not its own.

| Setting | Key | Effect |
| --- | --- | --- |
| Default model | `defaultModel` | the model of a new session whose request names none |
| Compaction model | `compactionModel` | the model that summarizes context; see [compaction](compaction.md) |
| Model overrides | `modelOverrides` | per-model context windows; see [model overrides](model-overrides.md) |
| Prompt cache retention | `promptCacheRetention` | `short` or `long`; see [prompt caching](prompt-caching.md) |
| Claude Code configuration | `readClaudeConfigs` | whether to read `.claude/commands`; see [prompt commands](prompt-commands.md) |

The server reads its settings each time it uses one, so a change needs no
restart. The default model applies to the next new session,
and prompt cache retention to the next request. Model overrides and the
compaction model apply to newly opened session runtimes or after `/reload`,
and `readClaudeConfigs` when a runtime next loads or reloads. Editing the file
by hand works the same way.

Client preferences in the same file, such as `theme` and `diffWrapLines`, stay
with each client. The server's access settings under `server` are changed with
`kit server expose` or by editing the file, not through the API.

## New sessions

A request to create a session may leave out its model and thinking level, and
`kit` and `kit -p` do when `--model` and `--thinking` aren't given. The server
then uses:

- **Model:** `defaultModel` when its provider has credentials, otherwise the
  first of `openai-codex/gpt-5.6-sol`, `openai/gpt-5.6-sol`, and
  `anthropic/claude-sonnet-4-6` with credentials, otherwise another model of
  the first of those providers with credentials, otherwise the first model in
  the catalog with credentials. With no credentials at all, creation fails
  with "no model is available; log in to a provider".
- **Thinking level:** the model's default.

Sessions that agents create with the `session` tool follow the same rules.

## Session API

| Operation | Purpose |
| --- | --- |
| `GET /v1/settings` | the settings, and the model and thinking level a new session would start with now |
| `PATCH /v1/settings` | change the settings the request carries and return the result |

A `PATCH` leaves absent fields unchanged. An empty string clears
`defaultModel` or `compactionModel`, and a model
override with a `contextWindow` of `0` is removed. Models must be in the
server's catalog, except when removing an override, and a server keeps at most
256 overrides. Other entries in the file are kept, and the file is replaced
atomically, through a symlink if it is one. A settings file the server can't
parse makes both operations report a conflict, with the reason in the
server's log, until it is fixed; new sessions meanwhile use the built-in
defaults.

Go clients use `Client.ServerSettings`, `UpdateServerSettings`, and
`SetModelContextWindow`.

## Clients

- The terminal client's `/model` picker edits context-window overrides on the
  attached server with `Ctrl+O`.
- The macOS app's Models settings still edit `settings.json` in its own Kit
  home, so they reach only a server using that home.
