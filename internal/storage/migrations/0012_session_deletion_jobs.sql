-- Reserve permanently deleted IDs to prevent a new session from reopening old
-- droid or attachment data if cleanup must be retried.
CREATE TABLE deleted_session_ids (
    id TEXT PRIMARY KEY,
    deleted_at TEXT NOT NULL,
    cleanup_pending INTEGER NOT NULL DEFAULT 1 CHECK (cleanup_pending IN (0, 1))
);

CREATE INDEX deleted_session_ids_pending_idx
    ON deleted_session_ids(cleanup_pending, deleted_at)
    WHERE cleanup_pending = 1;
