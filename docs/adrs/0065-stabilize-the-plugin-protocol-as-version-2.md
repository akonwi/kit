# 0065: Stabilize the plugin protocol as version 2

## Status

Accepted. Keeps the process model of
[ADR 0026](0026-scope-plugin-processes-and-route-plugin-ui.md) and the plugin
methods of [ADR 0041](0041-let-plugins-submit-session-messages.md),
[ADR 0049](0049-let-plugins-ask-attached-clients-to-open-web-pages.md), and
[ADR 0060](0060-share-observed-pull-request-checks-with-clients-and-plugins.md).
Client-hosted plugins ([ADR 0051](0051-let-attached-clients-host-session-plugins.md))
remain part of the session API and are unaffected.

## Context

Process plugins are trusted programs on the server's machine, launched by the
session runtime from a manifest and spoken to over stdio with JSON-RPC 2.0.
The pipe identifies the instance: a plugin needs no credential, server
address, or network access to reach Kit, and Kit learns of its exit directly.

The protocol's description is split across a target specification, a profile
of the subset Kit implements, and a hand-maintained JSON Schema. They disagree
with each other and with Kit. The schema requires parameters Kit rejects and
omits ones Kit accepts. The specification describes methods, fields, error
codes, and modes Kit refuses or no longer has, and requires every plugin to
accept JSON-RPC batches that Kit never sends. A plugin author cannot tell from
the documents what works. The protocol version has stayed at 1 while its
behavior changed, and the schema's published URL changes in place.

The session API solved the same problem with one Go owner for every wire
record, a generated schema checked for staleness, written compatibility rules,
and a version that changes when compatibility does (ADR 0031, 0032).

## Decision

### Transport and process model

Plugin protocol version 2 keeps the stdio transport: JSON-RPC 2.0, one
message per line, UTF-8, on the plugin's stdin and stdout, with stderr for
diagnostics. Discovery, the manifest's launch rules, the trust model, session
ownership, background initialization, scope across cwd changes and reload,
generation fencing, crash handling, failure notifications, and the
initialization and shutdown deadlines are as ADR 0026 describes. Plugins are
loaded by the session runtime on the server; print mode, `kit acp`, and other
clients neither launch nor speak to them.

Each line carries exactly one JSON-RPC object. Version 2 has no batches: Kit
never sends one and answers a batch with `-32600` and a null `id`, without
processing its members.

### One specification, one owner, generated schemas

`docs/plugin-protocol/v2.md` is the normative specification of version 2. It
describes exactly what Kit implements, including every method, parameter,
result, error, limit, and lifecycle rule, and nothing Kit does not implement.

The public Go package `api/pluginprotocol` owns every version 2 wire record:
the manifest, each method's params and result, each notification, and the
error codes and reasons. Where a record has the same wire shape as a session
contract record, such as pull request checks or footer style theme tokens, it
reuses the `api/contract` type rather than defining a copy. The plugin host
decodes what plugins send and encodes what it sends through these types.

A generator emits `docs/plugin-protocol/v2/protocol.schema.json` and
`docs/plugin-protocol/v2/manifest.schema.json` from the package. Both are
committed, and a test fails when they are stale. Their `$id`s are their
versioned raw URLs on `main`. Tests validate the messages the host sends and
the fixtures' messages against the generated schema.

### Selecting a version

A manifest may declare `"protocolVersion": 2`. Without it, the plugin speaks
version 1. Kit starts every conversation in the declared version, so a plugin
never has to accept a version it does not know. Version 1 behaves as it does
today and is removed in a later stage (see Migration).

### Initialization

`initialize` params are:

- `protocolVersion`: `2`.
- `kit`: `{ "version": "<Kit server version>" }`, for diagnostics.
- `instance`: `{ "id": "<opaque instance id>" }`, the identifier of this
  generation, the same one clients see on the plugin's toasts, dialogs,
  footer items, and commands. It changes with every generation.
- `context`: the session (`id`, nullable `name`) and project (absolute `cwd`,
  nullable Git context with optional pull request and checks), as today.

The plugin answers `{ "protocolVersion": 2 }`. Anything else fails
initialization.

### Methods

Plugin → Kit requests:

| Method | Result |
| --- | --- |
| `kit/commands/register`, `kit/commands/unregister` | canonical ID, null |
| `kit/tools/register`, `kit/tools/unregister` | canonical ID and model name, null |
| `kit/tool-calls/register-interceptor`, `kit/tool-calls/unregister-interceptor` | null |
| `kit/subagents/register`, `kit/subagents/unregister` | canonical name, null |
| `kit/footer/set`, `kit/footer/clear`, `kit/footer/hide`, `kit/footer/show` | canonical ID, null |
| `kit/ui/confirm`, `kit/ui/input`, `kit/ui/select` | the answer, or the cancellation value |
| `kit/session/submit-message` | `{ messageId, turnId }` |
| `kit/system/open-url` | null |

Plugin → Kit notifications: `kit/ui/toast`, `kit/cancel`.

Kit → plugin requests: `initialize`, `shutdown`, `kit/commands/execute`,
`kit/tools/execute`, `kit/tool-calls/before-execute`.

Kit → plugin notifications: `kit/cancel`, `kit/events/project.changed` (user
plugins, on a cwd change), `kit/events/session.changed` (rename),
`kit/events/git.changed`, `kit/events/agent.turn.started`,
`kit/events/agent.turn.completed`.

Their params, results, bounds, and semantics are those Kit implements today,
specified in `v2.md`. Version 2 has no header items, footer clicks, footer
URL actions, left-side footer items, system-prompt slots, or select filtering:
it has no such methods or members, and Kit rejects them as unknown.
`kit/footer/set` has no `side`, `clickable`, or `action`; `kit/ui/select` has
no `filterable`; `kit/session/submit-message` has no `sessionId`.

### Nested requests

`kit/ui/confirm`, `kit/ui/input`, and `kit/ui/select` accept an optional
`parentId`: the `id` of a Kit → plugin request the dialog serves, such as the
`kit/tool-calls/before-execute` request an approval dialog belongs to. When
that request ends, whether answered, cancelled, or abandoned with its
instance, Kit cancels the dialog, which then answers `-32001`. A `parentId`
naming no pending Kit request on this connection cancels the dialog at once.
Plugins may still cancel dialogs themselves with `kit/cancel`.

### Errors

| Code | Meaning |
| ---: | --- |
| `-32600` | Invalid request, including a batch |
| `-32601` | Unknown method |
| `-32602` | Invalid params; the message names the rule broken |
| `-32603` | Internal error |
| `-32000` | A plugin's command or tool failure |
| `-32001` | Cancelled |
| `-32002` | The instance's generation is stopping, stopped, or replaced |
| `-32003` | Conflict, such as a duplicate contribution or reused idempotency key |
| `-32005` | A limit was exceeded |
| `-32006` | Try again later; `data.reason` is `busy` (too many concurrent requests), `session_busy` (submit-message admission), or `rate_limited` (open-url) |

`v2.md` lists the codes each method can return. Error messages are safe to
display; `data`, when present, is structured.

### Compatibility within a version

Within version 2 Kit may:

- add optional members to objects it sends;
- add notifications it sends;
- add plugin → Kit methods and optional params.

Plugins ignore members and notifications they do not know. The generated
schema describes the objects Kit sends as open and the objects plugins send as
closed, so a validator written against an older version 2 schema accepts
newer Kit messages.

Removing or renaming anything, adding a required member, tightening an
accepted shape or bound, changing a result or error, or adding a Kit → plugin
request needs a new protocol version. A new version gets its own
specification and schema directory, so published schema URLs never change
meaning. `docs/plugin-development.md` is the checklist for changing the
protocol, as `docs/api-development.md` is for the session API.

### Authoring helper

The repository carries a TypeScript helper for plugins as one self-contained,
dependency-free file, `sdk/plugin/kit-plugin.ts`, that works under Bun and
Node. A plugin copies the file into its own source. It is not published to a
package registry. The file states the protocol version and Kit version it
came from, and the version 2 compatibility rules keep an older copy working
with a newer Kit that speaks version 2.

A plugin exports a setup function and runs it with `runPlugin`:

```ts
import { runPlugin, t, type Kit } from "./kit-plugin.ts";

export async function setup(kit: Kit) {
  await kit.tools.register({
    id: "word_count",
    description: "Count the words in a file",
    input: t.object({ path: t.string() }),
    async execute(input, ctx) {
      return `${await countWords(input.path, ctx.signal)} words`;
    },
  });
  await kit.interceptor.register(async (call, ctx) => {
    if (call.name !== "bash") return "allow";
    const ok = await ctx.ui.confirm({ title: "Run this command?" });
    return ok ? "allow" : { reject: "The user rejected this command." };
  });
}

if (import.meta.main) runPlugin(setup);
```

The helper:

- groups capabilities by kind: `kit.tools`, `kit.commands`, `kit.subagents`,
  `kit.footer`, `kit.interceptor`, `kit.ui`, `kit.session`, `kit.system`,
  and `kit.on` for events. Each `register` takes the contribution and its
  handler together and resolves to a handle with the canonical identifiers
  and `unregister()`;
- answers `initialize`, then awaits setup, so no request precedes
  initialization. A failure during setup exits the plugin with the error on
  stderr. A later unhandled rejection is written to stderr and shown as an
  error toast;
- gives each Kit request's handler a context with an `AbortSignal` that
  `kit/cancel` aborts and dialog methods that set `parentId` to that request.
  The top-level `kit.ui` methods have no parent. A plugin request accepts a
  signal whose abort sends `kit/cancel`;
- keeps `kit.session` and `kit.project` current from events before calling
  listeners, and ignores members and notifications it does not know;
- redirects `console` output and other writes to stdout onto stderr, so only
  protocol messages reach stdout;
- runs setup's returned disposer on `shutdown` or when stdin closes, then
  exits;
- declares tool inputs with a schema builder, `t`, that offers only the
  keywords of Kit's tool schema subset and infers the handler's input type.
  A raw schema with an explicit input type is also accepted. A tool handler
  returns a full tool result, or a string as its single text part.

Types for protocol messages are generated from the version 2 schema into a
marked region of the file, and a check fails when they are stale. A second
file, `sdk/plugin/kit-plugin-testing.ts`, runs a plugin's setup against an
in-memory Kit so plugins can test their handlers.

Kit's built-in `kit-customization` skill tells models that a TypeScript
plugin copies `sdk/plugin/kit-plugin.ts` from the Kit repository at the
release tag of the running Kit version.

There is no maintained helper for other languages. The specification and
schema are enough to write one, and the repository's Python fixtures are test
fixtures, not a library.

### Migration

Version 2 ships alongside version 1. The version 1 specification and schemas
stay at their current paths and URLs, unchanged, while Kit supports version 1.
Once the personal plugins in `dot-kit`
use version 2, a version 1 manifest produces a session warning naming the
plugin and the migration guide. A final stage removes version 1: Kit rejects a
manifest without `"protocolVersion": 2` with a diagnostic, and the version 1
specification, implementation profile, and schemas leave the repository.

The session API needs no protocol change for this decision. The interaction
record's `filterable` member exists only for version 1 select requests; it is
removed with version 1, through the session API's compatibility process.

## Consequences

- One document and one schema say what a plugin can do, and both are checked
  against Kit's code.
- Plugins keep a credential-free, network-free, same-machine transport.
- Existing plugins run unchanged until version 1 is removed, and each
  plugin moves by declaring version 2 in its manifest and dropping the
  members version 2 does not have.
- Plugin authors write less: no batch handling, and approval dialogs close
  with their interceptor call without cancellation bookkeeping.
- Kit maintains two protocol versions during the deprecation period.
- A Kit release without version 2 rejects a manifest that declares it, as it
  rejects any unknown manifest member, and reports the manifest as invalid.
- Plugins and clients still use two protocols; they share record types where
  shapes coincide, but not a transport or a version.
- Changing the plugin protocol now has a written compatibility bar and a
  version boundary, which slows breaking changes on purpose.
