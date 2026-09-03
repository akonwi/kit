# 0002: Internalize the droids agent core during the rewrite

## Status

Accepted

## Context

The native rewrite initially consumed `github.com/akonwi/droids` as an external
Go module. Kit and droids are both early in their development, and the rewrite
is now discovering agent-core requirements—durable replay metadata, daemon-safe
credential refresh, provider errors, compaction behavior, and session
orchestration—in the same iterations that define Kit's runtime.

Releasing and updating a separate module for every coordinated change adds an
artificial boundary before either API has stabilized. In particular, OpenAI
Codex needs a credential lifecycle designed around Kit's persistent daemon:
callers should be able to pass credentials directly, the provider should
refresh expiring access automatically, and Kit should remain responsible for
any durable credential storage.

## Decision

Seed a private `internal/droids` package from `github.com/akonwi/droids` commit
`69dc707` and remove the external module dependency for now.

The copied package retains the droids provider, loop, message, stream, tool,
compaction, storage, and optional MCP layers plus their tests. Its nested
`openaicodex` package retains protocol-only OAuth helpers. Kit server-side
runtime packages import `github.com/akonwi/kit/internal/droids`; clients still
consume only Kit-owned session contracts and wire projections.

This is a source fork, not vendoring with an implicit update mechanism. The
source commit is recorded in `internal/droids/README.md`. Future upstream or
outbound synchronization must be explicit and reviewed rather than performed
by an automated dependency update.

### Codex credential lifecycle

`OpenAICodex` supports two mutually exclusive configuration forms:

```go
// Simple/in-memory use.
droids.OpenAICodex{
    Credentials: droids.OpenAICodexCredentials{...},
}

// Application-owned durable storage.
droids.OpenAICodex{
    CredentialStore: store,
}
```

Before each request, the provider:

1. loads the current credential generation from the configured store, when
   present;
2. derives safe account, routing, and expiry metadata when available;
3. refreshes credentials that are missing an access token or are near expiry;
4. serializes concurrent refresh attempts;
5. retains refreshed credentials in memory;
6. compare-and-swaps rotated credentials through the store before using them.

The store is authoritative and returns an opaque revision on every load. A
failed save prevents the model request, keeps the fresh credential in memory,
and retries persistence on the next resolution. A revision conflict discards
the stale refresh result and reloads the newer login, logout, or concurrent
refresh generation. This prevents an old refresh-token chain from recreating a
deleted credential or overwriting a same-account login. Direct credentials
refresh in memory but are intentionally not durable across daemon restarts.
Refresh is a bounded provider-owned single-flight operation: canceling one model
request stops only that caller's wait and does not cancel credential maintenance
needed by other sessions.

Codex assistant messages carry a one-way credential-scope hash. Kit persists it
inside the message projection, and the provider refuses to replay any prior
Codex assistant message whose scope is absent or belongs to another account.
This prevents encrypted reasoning, function-call identities, and plaintext
history from silently crossing ChatGPT accounts.

The OAuth helper owns protocol mechanics only. It does not choose Kit's file
format, open UI, or bypass an application credential store.

## Consequences

Positive:

- Kit and its agent core can evolve atomically while the rewrite stabilizes;
- no local `replace` directive or unpublished droids release is needed;
- agent-core tests run as part of Kit's ordinary Go suite;
- Codex offers a simple direct configuration and daemon-safe automatic refresh;
- persistence remains an explicit Kit-owned port;
- renderer and wire boundaries remain independent from droids types.

Trade-offs:

- fixes made in Kit do not automatically reach the standalone droids repo;
- imported source materially increases the Kit repository and test surface;
- future reunification requires deliberate API and history reconciliation;
- package privacy means other modules cannot consume Kit's fork;
- direct in-memory refresh can be lost on process restart unless Kit supplies a
  credential store.

## Follow-up

- implement native TUI and browser login/logout presentation over Kit's
  `~/.kit-v2` credential store and headless auth commands;
- decide after the rewrite stabilizes whether to extract droids again, maintain
  it independently, or upstream selected changes.

## Related

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [`../../internal/droids/README.md`](../../internal/droids/README.md)
- External source commit `github.com/akonwi/droids@69dc707`
