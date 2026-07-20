# Session compaction storage

Compaction is both a runtime context boundary and a durable storage boundary.

## Primary sessions

When a session compacts, Kit atomically rewrites its JSONL file instead of appending the compaction to the complete historical log. The replacement contains:

1. The session header, updated with the current name, cwd, model, and thinking level.
2. Entries belonging to the turns retained by compaction.
3. The synthetic compaction summary entry.

Entries before the first retained turn are removed. Mutable metadata entries are folded into the header rather than retained separately. Superseded compaction entries are removed. Remaining entries are re-linked into a valid parent chain, and subsequent writes continue from the compaction entry. If no turns are retained, the file contains only the header, active sub-agent references, and compaction summary.

Updating mutable metadata in the header preserves current session state while removing its historical change records. Rewritten headers mark that metadata as authoritative through the compaction boundary, so a dedicated summary model cannot replace the active session model during reconstruction. Active sub-agent start references are carried forward so their separately persisted conversations remain discoverable after restart.

## Sub-agent sessions

Sub-agent compaction entries already contain the retained turns. Writing one atomically replaces the child JSONL history with its header and latest compaction entry. Entries written after that compaction append normally.

## Failure behavior

Rewrites use a synced temporary file in the same directory followed by a rename and a best-effort directory sync. The in-memory storage state is updated only after the replacement succeeds, so a failed rewrite leaves the previous session file and state available.

All primary and child session mutations acquire a renewable per-file lock shared across Kit processes. Append paths refresh their cached state from disk while holding that lock before assigning entry IDs or rewriting history. A cached writer rejects missing files and stale full-session replacements instead of recreating or overwriting externally changed sessions. Session listing reads summaries without mutating live storage state.
