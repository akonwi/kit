CREATE TABLE droid_state (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    conversation_id TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    last_record_sequence INTEGER NOT NULL DEFAULT 0
        CHECK (last_record_sequence >= 0),
    last_event_sequence INTEGER NOT NULL DEFAULT 0
        CHECK (last_event_sequence >= 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE records (
    record_kind TEXT NOT NULL,
    record_id TEXT NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('runtime', 'history')),
    sequence INTEGER,
    version INTEGER NOT NULL CHECK (version >= 1),
    payload BLOB NOT NULL,
    created_revision INTEGER NOT NULL CHECK (created_revision >= 0),
    updated_revision INTEGER NOT NULL
        CHECK (updated_revision >= created_revision),
    PRIMARY KEY (record_kind, record_id),
    UNIQUE (sequence),
    CHECK (
        (scope = 'runtime' AND sequence IS NULL) OR
        (scope = 'history' AND sequence IS NOT NULL AND sequence >= 1)
    )
) WITHOUT ROWID;

CREATE INDEX records_scope_sequence_idx
    ON records(scope, sequence);

CREATE INDEX records_kind_sequence_idx
    ON records(scope, record_kind, sequence);

CREATE TRIGGER records_history_immutable_update
BEFORE UPDATE ON records
WHEN OLD.scope = 'history'
BEGIN
    SELECT RAISE(ABORT, 'historical record is immutable');
END;

CREATE TRIGGER records_history_immutable_delete
BEFORE DELETE ON records
WHEN OLD.scope = 'history'
BEGIN
    SELECT RAISE(ABORT, 'historical record is immutable');
END;

CREATE TABLE events (
    sequence INTEGER PRIMARY KEY CHECK (sequence >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    kind TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version >= 1),
    payload BLOB NOT NULL,
    occurred_at TEXT NOT NULL
) WITHOUT ROWID;

CREATE INDEX events_revision_idx ON events(revision, sequence);
