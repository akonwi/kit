# 0020: Persist sessions as append-only JSONL entry logs

## Status
Accepted

## Context

Kit currently persists each session as one JSON file containing a full serialized `Session` object with `turns: Turn[]`.

That creates several problems:

- every save rewrites the full session file
- write cost grows with session length
- overlapping writes are easier to trigger
- compaction is destructive because the persisted shape mirrors the current in-memory `Turn[]`

We want to move toward Pi's storage model, where sessions are persisted as append-only JSONL entry logs and current runtime state is reconstructed from those entries.

## Decision

Kit will persist sessions as JSONL files:

```text
~/.kit/sessions/<id>.jsonl
```

The persisted format is an append-only entry log.

Runtime sessions remain turn-based, but the persisted format does **not** store a nested `Turn[]` structure.

### Entry model

A session file begins with one header record:

- `type: "session"`
- session id
- creation timestamp
- cwd
- fixed fork metadata such as `parentSessionId` and `forkedFromTurnId`

Subsequent lines are typed entries with:

- `id`
- `parentId`
- `timestamp`

The initial implementation uses a linear parent chain, but `parentId` keeps the format open to future tree semantics.

### Turn reconstruction

Actual message entries persist:

- `type: "message"`
- `turnId`
- `message` payload without `turnId`

`turnId` on message entries is sufficient to reconstruct runtime turns.

On load, Kit rebuilds `Turn[]` by:

1. resolving the active visible entry sequence
2. grouping consecutive message entries by `turnId`
3. materializing runtime `Turn` objects from that grouping

This keeps turn-based rendering and runtime ergonomics without forcing the disk format to mirror `Turn[]` directly.

### Non-turn entries

Some persisted entries are part of the conversation timeline but are not actual turns.

These include:

- `compaction`
- `handoff_summary`
- metadata entries like `session_info`, `model_change`, and `thinking_level_change`

Compaction and handoff summaries do **not** persist a `turnId`.

When Kit reconstructs transcript/runtime state, those entries may be surfaced as synthetic singleton turns for compatibility with the current UI, but they are not modeled as true turns in storage.

### Compaction

Compaction becomes append-only.

Instead of rewriting older turns out of the file, Kit appends a `compaction` entry containing:

- the synthesized summary message
- the first kept entry id
- compaction counts/metadata

On load, the latest compaction entry defines the visible prefix summary plus the kept tail of the session.

This preserves old history on disk while keeping active runtime context compact.

### Migration compatibility

Kit will continue to read legacy `.json` session files during migration.

When a legacy session is loaded, Kit may rewrite it into the new `.jsonl` format and remove the old `.json` file.

## Consequences

### Positive

- normal session persistence becomes append-only
- save cost no longer scales with full transcript size
- compaction becomes non-destructive in storage
- runtime/UI can stay turn-based
- the storage model is closer to Pi and easier to evolve

### Trade-offs

- load logic becomes more complex because current runtime state is derived
- transcript/runtime reconstruction must interpret compaction and summary entries
- some current runtime concepts remain a read model rather than a direct serialization of storage

## Related

- `docs/adrs/0002-storage-paths.md`
- `docs/adrs/0004-turn-based-session-model.md`
- `docs/adrs/0009-compaction-strategy.md`
- `src/storage/session-storage.ts`
