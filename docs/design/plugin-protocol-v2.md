# Plugin protocol version 2: drift, migration map, and stages

Working document for [#128](https://github.com/akonwi/kit/issues/128) and
[ADR 0065](../adrs/0065-stabilize-the-plugin-protocol-as-version-2.md). It
records where the version 1 documents disagree with Kit, what changes for a
plugin moving to version 2, and the order of the work.

## Where the version 1 documents disagree with Kit

| Topic | `v1.md` / `protocol.schema.json` | Kit |
| --- | --- | --- |
| `kit/session/submit-message` params | `sessionId` required; no `idempotencyKey` | rejects `sessionId`; accepts `idempotencyKey` (ADR 0041) |
| `kit/session/submit-message` result | `NullResult` | `{ messageId, turnId }` |
| Busy submission | `-32006` "temporarily busy" | `-32006` with `data.reason: "session_busy"` |
| `kit/system/open-url` | platform opener | asks the session's attached clients (ADR 0049); rate limit `-32006` `rate_limited` |
| `kit/header/*`, `kit/footer/click`, `kit/system-prompt/*` | specified | method not found |
| `kit/footer/set` `side: "left"`, `clickable: true`, `action` | specified | rejected with `-32601` |
| `kit/ui/select` `filterable` | specified | accepted, ignored |
| Batches | both sides must accept | Kit never sends one |
| `-32004` "active session changed" | specified | never sent; plugins are session-scoped |
| Scope | user plugins survive "session switches" | one instance per session (ADR 0026) |
| Web mode (`ui.toast.requested`, `activate_chrome_contribution`) | specified | the browser client is a session API client; plugins see none of this |
| `kit/events/session.changed` | session switches and renames | renames only |
| Limits | partly in `v1.md`, partly in `native-v2.md` | — |

`dot-kit`'s `autoresearch` sets its footer item with `side: "left"`, which
Kit rejects; its helper logs the error to stderr, so the item never appears.

## Moving a plugin from version 1 to version 2

| Change | Who is affected today |
| --- | --- |
| Add `"protocolVersion": 2` to `plugin.json`; point `$schema` at `docs/plugin-protocol/v2/manifest.schema.json` | every plugin |
| Answer `initialize` with `{ "protocolVersion": 2 }`; accept the new `kit` and `instance` members | every plugin (cowork, gdrive, tool-approval, and the fixtures check for 1) |
| Stop sending `side`, `clickable`, `action` on `kit/footer/set` | autoresearch helper (`side`, `clickable`) |
| Stop sending `filterable` on `kit/ui/select` | none known |
| Stop sending `sessionId` on `kit/session/submit-message` | none known |
| Drop batch handling | optional; no plugin sends batches |
| Pass `parentId` on dialogs opened inside a Kit request (optional) | tool-approval benefits; it can stop forwarding cancellation itself |
| Ignore unknown members and notifications | plugins that validate strictly |

Plugins built on the TypeScript helper get all of this by copying the helper
and updating the manifest. Porting a `dot-kit` TypeScript plugin also means
deleting its own JSON-RPC endpoint and moving to the helper's namespaced
registration: `kit.registerTool({ name, parameters, execute(toolCallId,
params, signal) })` becomes `kit.tools.register({ id, input, execute(input,
ctx) })`, with `ctx.signal` for cancellation.

## What stays the same

Discovery paths and order, the manifest's launch rules, trust, session
ownership, background initialization, cwd and reload scope, generation
fencing, crash handling and failure notifications, deadlines (10 s
initialization, 2 s shutdown, 2 s cleanup), every contribution and payload
bound, tool schema subset, interceptor semantics, dialog semantics, turn and
Git events, and client-hosted plugins (ADR 0051). Print mode and `kit acp`
are unaffected: plugins run in the server's session runtime and these
commands are ordinary session clients.

## Stages

Each stage is one or more commits carrying `Refs #128`, except the last,
which carries `Fixes #128`. No stage changes the session API protocol
(it stays 50) until stage 5's optional `filterable` removal.

1. **Types, schema, and version 2 in the host.** Add `api/pluginprotocol`
   with every version 2 record (reusing `api/contract` types where shapes
   coincide), the schema generator, and the committed `v2/` schemas with a
   staleness test. Manifests accept `protocolVersion`. The host speaks
   version 2 to version 2 manifests: no batches, the pruned surface, error
   reasons, `parentId`, and the new `initialize` members. Version 1 is
   untouched. Write `v2.md` and `docs/plugin-development.md`.
   Tests: schema staleness; every message the host sends in the version 2
   tests validates against the schema; real-subprocess tests for each
   method with the repository fixtures ported to version 2; batch rejection;
   rejection of each pruned method and member; `parentId` cancellation when
   the parent is answered, cancelled, and revoked, and for an unknown parent;
   one version 1 fixture kept to prove version 1 is unchanged;
   `go test -race` for `internal/plugin` and `internal/server`.
2. **TypeScript helper.** Add the self-contained `sdk/plugin/kit-plugin.ts`
   (runtime, `t` schema builder, and generated types in a marked region with
   a staleness check) and `sdk/plugin/kit-plugin-testing.ts`. Port one
   fixture to it. Add the helper to the built-in `kit-customization` skill:
   TypeScript plugins copy `kit-plugin.ts` from the release tag of the
   running Kit version.
   Tests: Bun tests through the in-memory Kit for each capability; `parentId`
   set by handler dialogs and absent from top-level ones; cancellation both
   ways; stdout writes redirected to stderr; setup failure exits nonzero;
   shutdown and stdin EOF run the disposer; unknown members and notifications
   ignored; `t` produces only subset keywords and infers input types
   (type-level tests); a real-subprocess daemon test of the ported fixture;
   the skill's text in `internal/skills` tests.
3. **Personal plugins.** Port `dot-kit` (autoresearch, cowork, gdrive,
   tool-approval) to version 2, in that repository, outside this one.
4. **Deprecation.** A version 1 manifest produces a session warning naming
   the plugin and the migration guide. Tests: the warning's exact text and
   that version 1 plugins still load.
5. **Removal.** Kit rejects manifests without `"protocolVersion": 2`;
   remove the version 1 code paths, `v1.md`, `native-v2.md`, and the version 1
   schemas; move the macOS footer token test to the version 2 schema.
   Optionally remove the session contract's `filterable` interaction member
   through the compatibility gate. `Fixes #128`.
