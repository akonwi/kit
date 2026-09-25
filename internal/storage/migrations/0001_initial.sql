CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    cwd TEXT NOT NULL,
    name TEXT,
    persistent INTEGER NOT NULL DEFAULT 1 CHECK (persistent IN (0, 1)),
    parent_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    model_provider TEXT,
    model_id TEXT,
    thinking_level TEXT,
    droid_initialized_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    archived_at TEXT,
    CHECK (parent_session_id IS NULL OR parent_session_id <> id)
);

CREATE INDEX sessions_cwd_updated_idx
    ON sessions(cwd, updated_at DESC);
CREATE INDEX sessions_parent_idx
    ON sessions(parent_session_id)
    WHERE parent_session_id IS NOT NULL;

CREATE TRIGGER sessions_reject_parent_cycle_on_insert
BEFORE INSERT ON sessions
WHEN NEW.parent_session_id IS NOT NULL
BEGIN
    SELECT CASE WHEN EXISTS (
        WITH RECURSIVE ancestors(id) AS (
            SELECT NEW.parent_session_id
            UNION
            SELECT sessions.parent_session_id
            FROM sessions
            JOIN ancestors ON sessions.id = ancestors.id
            WHERE sessions.parent_session_id IS NOT NULL
        )
        SELECT 1 FROM ancestors WHERE id = NEW.id
    ) THEN RAISE(ABORT, 'session parent cycle') END;
END;

CREATE TRIGGER sessions_reject_parent_cycle_on_update
BEFORE UPDATE OF parent_session_id ON sessions
WHEN NEW.parent_session_id IS NOT NULL
BEGIN
    SELECT CASE WHEN EXISTS (
        WITH RECURSIVE ancestors(id) AS (
            SELECT NEW.parent_session_id
            UNION
            SELECT sessions.parent_session_id
            FROM sessions
            JOIN ancestors ON sessions.id = ancestors.id
            WHERE sessions.parent_session_id IS NOT NULL
        )
        SELECT 1 FROM ancestors WHERE id = NEW.id
    ) THEN RAISE(ABORT, 'session parent cycle') END;
END;
