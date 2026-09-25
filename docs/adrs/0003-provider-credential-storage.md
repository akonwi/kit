# 0003: Keep provider credentials in a Kit-owned atomic file

## Status

Accepted

## Context

Kit's persistent daemon needs credentials after clients detach and must retain
rotated OAuth refresh tokens across process restarts. The agent core can perform
provider-specific refresh protocol operations, but it must not choose Kit's
storage location, mutate a shared JSON document without coordination, or own
login/logout presentation.

Provider credentials live in `~/.kit/auth.json`, keyed by provider ID. Concurrent
daemon refreshes and login/logout clients create a stale-write risk: an old
refresh result must never overwrite a newer same-account login or recreate a
credential deleted by logout.

## Decision

Kit stores provider credentials in
`~/.kit/auth.json` (or `KIT_HOME/auth.json`). `internal/auth` owns this file.
It is machine-managed secret state, not a general settings surface.

The document is a provider-keyed JSON object. Existing installations
reauthenticate through the login UX; no credential importer is required. When
reusing an existing `~/.kit`, legacy provider and MCP auth formats
must not block reauthentication or require manual file deletion. Explicit login
may replace the selected legacy credential while preserving unrelated entries;
malformed storage still fails closed. The OpenAI Codex entry uses the established
field names plus optional Kit metadata:

```json
{
  "openai-codex": {
    "type": "oauth",
    "access": "...",
    "refresh": "...",
    "expires": 1900000000000,
    "accountId": "...",
    "idToken": "...",
    "fedRAMP": false,
    "revision": "v1:..."
  }
}
```

Unknown provider entries are preserved. Files are bounded to 1 MiB. The home directory is mode `0700`; the credential and lock files are
mode `0600`. Symlink and other non-regular destinations are rejected.

Every operation takes an inter-process advisory lock beside the auth file.
Writes use a same-directory temporary file, file sync, atomic rename, and
directory sync. Malformed existing storage is reported and never overwritten.

### Credential generations

Each newly installed or refreshed OAuth entry receives a cryptographically
random, non-secret revision. Legacy Codex entries without a revision receive a
stable hash-derived revision over canonical credential fields until their first
successful write, so JSON reformatting does not create a false generation.

The droids credential-store port uses compare-and-swap semantics:

1. load credentials and their revision;
2. refresh outside the file lock;
3. save only if the non-empty loaded revision remains current;
4. never create an absent credential through the refresh path;
5. on conflict, discard the stale refresh result and reload authoritative
   credentials.

Deletion is represented by the absence of an entry. Therefore, a refresh based
on any prior non-empty revision cannot recreate a logged-out credential. An
intentional login uses a separate replacement operation that always creates a
new revision.

The Codex and Anthropic providers check the store generation before each model
request. This lets a long-running daemon observe login, replacement, and logout
without continuing indefinitely with a cached credential. Anthropic entries may
contain either an API key or a Claude Pro/Max OAuth generation, never both.

### Headless authentication

`kit login openai-codex` runs the OAuth device flow, prints only the fixed
verification URL and validated short-lived user code, polls with cancellation,
and installs the result as a new credential generation. `kit logout
openai-codex` deletes the entry. `kit auth status` lists only printable provider
IDs, credential types, and the active daemon source—never token or account
values. The native TUI also offers Claude Pro/Max browser OAuth with a loopback
callback and manual redirect/code fallback. It installs credentials through the
same replacement and daemon-generation guard used by other login methods.

### Environment credentials

Explicit `OPENAI_CODEX_*` credentials and `ANTHROPIC_OAUTH_TOKEN` take
precedence when the daemon starts. They are direct in-memory credentials rather
than file-store credentials; refresh rotation is not durable. Environment
changes still require a daemon restart. This path is intended for development and externally managed
secret injection.

The daemon publishes its non-secret credential source in the additive v1 local
registry. Login and logout hold the startup lock while checking that registry.
When no live daemon exists they also hold the lifetime lock through the auth
write, preventing a child from starting between the check and mutation. Stale
registrations are cleared only while both locks prove there is no owner. A
running environment-backed daemon causes login/logout to fail with an
actionable restart message rather than falsely claiming the stored generation
is active.

## Consequences

Positive:

- OAuth rotation survives normal daemon restarts;
- login/logout and refresh cannot silently clobber each other;
- unrelated provider credentials survive Codex updates;
- malformed or adversarial filesystem state fails closed;
- the agent core remains independent of Kit's concrete file format;
- `KIT_HOME` keeps development and tests isolated from real credentials.

Trade-offs:

- every stored-credential model call performs a small locked file read;
- advisory locks coordinate Kit processes but are not a defense against
  arbitrary same-user processes;
- auth writes replace formatting and top-level key order;
- existing users may need to reauthenticate providers and MCP servers rather
  than import their saved credentials;
- browser-client auth presentation and headless Anthropic OAuth remain separate
  work.

## Related

- [0001: Native Go application architecture](./0001-native-go-architecture.md)
- [0002: Internalize the droids agent core](./0002-internalize-agent-core.md)
