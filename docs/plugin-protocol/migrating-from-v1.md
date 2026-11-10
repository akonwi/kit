# Migrating a plugin from protocol version 1 to version 2

Kit runs plugins whose manifest has no `protocolVersion` as protocol
version 1, and a session that loads one shows a warning naming it and linking
here. Version 1 will be removed in a later release, so move each plugin to
[version 2](v2.md). Most plugins need only the manifest and `initialize`
changes; the rest applies only to plugins that use what version 2 removes.

## Every plugin

1. In `plugin.json`, add `"protocolVersion": 2` and point `$schema` at
   `https://raw.githubusercontent.com/akonwi/kit/main/docs/plugin-protocol/v2/manifest.schema.json`.
2. Answer `initialize` with `{ "protocolVersion": 2 }`. Its params now also
   carry `kit.version` and `instance.id`; ignore them if you don't need them.
3. Ignore members and notifications you don't know in what Kit sends. Kit
   rejects request params with members it doesn't know, with `-32602`.

## What version 2 removes

Kit answers a removed method with `-32601` and a removed member with `-32602`.
It never sends the removed click requests.

| Version 1 | In version 2 |
| --- | --- |
| Batches (JSON arrays) | Send one message per line. Kit answers an array with `-32600` and never sends one. |
| `kit/header/set`, `clear`, `hide`, `show`, and `kit/header/click` | Use a footer item. |
| `side`, `clickable`, and `action` on `kit/footer/set` | Omit them. Footer items appear at the bottom right. Offer the click's behaviour as a command. |
| `kit/footer/click` | Offer the behaviour as a command. |
| `kit/system-prompt/set` and `clear` | No replacement. Move standing instructions to a context file or skill. |
| `filterable` on `kit/ui/select` | Omit it. |
| `sessionId` on `kit/session/submit-message` | Omit it; a plugin instance belongs to one session. The result is `{ messageId, turnId }`. |
| Error `-32004` (active session changed) | Drop its handling; Kit never sends it. |

## What version 2 adds

- **`parentId` on dialogs.** A `kit/ui/confirm`, `kit/ui/input`, or
  `kit/ui/select` opened while serving a Kit request, such as an approval
  dialog inside `kit/tool-calls/before-execute`, can name that request's `id`
  as `parentId`. Kit then cancels the dialog when the request ends, so the
  plugin no longer has to.
- **Error reasons.** `-32006` carries `data.reason`: `busy` (too many requests
  in flight; retry later), `session_busy` (submit-message), or `rate_limited`
  (open-url). `-32000` with `data.reason` `interactivity_unavailable` means
  Kit cannot show dialogs.

## TypeScript plugins

A plugin that writes its own JSON-RPC needs only the changes above. To use
Kit's helper instead, copy `sdk/plugin/kit-plugin.ts`, and
`sdk/plugin/kit-plugin-testing.ts` for tests, from the Kit repository at the
tag of your Kit version (see [`sdk/plugin/README.md`](../../sdk/plugin/README.md)).
The helper speaks version 2 for you, but adopting it rewrites the plugin's
protocol code:

- Delete the plugin's JSON-RPC endpoint and its `initialize` handling.
- Move every registration and call to the helper's namespaces, such as
  `kit.commands`, `kit.tools`, `kit.footer`, and `kit.ui`. Each handler is
  passed with its registration, for example
  `kit.tools.register({ id, input, execute(input, ctx) })`, with the input
  schema built with `t`.
- Take cancellation from `ctx.signal`, and open dialogs that belong to a
  request with `ctx.ui`, which sets `parentId`.

## Checking a migrated plugin

Validate each line the plugin writes against
[`v2/protocol.schema.json`](v2/protocol.schema.json), then reload a session
and confirm the plugin starts without a failure notification. With the
helper, `startTestPlugin` from `kit-plugin-testing.ts` runs the plugin
against an in-memory Kit that checks identifiers, required text, tool schemas, footer
targets, URLs, and idempotency keys, but not Kit's byte and count limits, so
still try the plugin in Kit.
