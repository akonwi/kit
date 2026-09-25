CREATE TABLE direct_bash_history (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    execution_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    command TEXT NOT NULL,
    cwd TEXT NOT NULL,
    status TEXT NOT NULL,
    output TEXT NOT NULL DEFAULT '',
    exit_code INTEGER,
    exclude_from_context INTEGER NOT NULL DEFAULT 0,
    truncated INTEGER NOT NULL DEFAULT 0,
    timed_out INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    completed_at TEXT NOT NULL,
    PRIMARY KEY (session_id, execution_id)
);

CREATE INDEX direct_bash_history_session_sequence_idx
    ON direct_bash_history(session_id, sequence);
