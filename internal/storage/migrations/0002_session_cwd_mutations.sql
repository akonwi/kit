CREATE TABLE session_cwd_mutations (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    mutation_id TEXT NOT NULL,
    target_path TEXT NOT NULL,
    previous_cwd TEXT NOT NULL,
    cwd TEXT NOT NULL,
    changed INTEGER NOT NULL CHECK (changed IN (0, 1)),
    created_at TEXT NOT NULL,
    PRIMARY KEY (session_id, mutation_id)
);

CREATE INDEX session_cwd_mutations_recent_idx
    ON session_cwd_mutations(session_id, created_at DESC);
