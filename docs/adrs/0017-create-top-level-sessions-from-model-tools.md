# 0017: Create top-level sessions from model tools

## Status

Accepted

## Context

Persistent sessions can discover and query other top-level sessions through
`peer_session`, but they cannot create a new peer. Subagents are unsuitable for
this purpose because they are bounded child executions owned by one parent,
whereas peer sessions have independent durable identity and lifecycle.

Session creation is authoritative server state. A model tool must not call the
client RPC API or write session storage directly.

## Decision

Kit exposes a separate `create_session` model tool for persistent top-level
sessions. It delegates through a narrow server-owned capability to the session
manager's existing creation and prompt-admission paths.

The tool:

- requires an existing absolute `cwd` and a non-empty `name`;
- optionally accepts an initial prompt;
- creates only persistent top-level sessions;
- always uses the configured default model, then Kit's default available model;
- uses the selected model's default thinking level;
- may admit the initial prompt asynchronously and return its run ID;
- returns the created session's durable ID and effective configuration;
- never switches or replaces the calling session.

Created sessions persist the caller as their parent provenance. They remain
ordinary discoverable top-level sessions and receive the same session tools as
other persistent sessions, including `create_session`.

Creation with an initial prompt is transactional at the tool boundary. If
prompt admission fails, Kit deletes the newly created session and returns a
failure. If rollback also fails, the result reports both failures. The tool is
not exposed to temporary sessions.

The capability is injected alongside `peer_session`, but remains a distinct
tool because creation has a focused schema and lifecycle. The session manager continues to validate workspace paths, model identity,
thinking compatibility, and session names.

## Required properties

- The tool cannot create temporary sessions.
- A temporary or unknown owner cannot use the capability.
- Created sessions are ordinary durable top-level sessions discoverable through
  `peer_session` and normal clients.
- The tool does not bypass manager serialization, validation, or persistence.
- An initial prompt runs in the created session, not the caller.
- Prompt admission failure rolls back the newly created session.
- Tool results do not expose credentials, provider bindings, stores, or runtime
  internals.

## Consequences

Models can establish independent durable workspaces and then coordinate with
them through existing peer-query tools. This also permits models to create
persistent state, so the capability is limited to already persistent sessions
and uses the same bounded tool execution and session validation as other
server-owned operations.

## Related

- [ADR 0005: Define Droids semantic forking](0005-droids-semantic-forking.md)
- [ADR 0006: Make Droids the session data authority](0006-droids-as-session-data-authority.md)
- [ADR 0014: Route durable peer-session queries](0014-route-durable-peer-session-queries.md)
