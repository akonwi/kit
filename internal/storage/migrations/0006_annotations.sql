CREATE TABLE annotation_sequences (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    next_id INTEGER NOT NULL CHECK(next_id > 0)
);

CREATE TABLE annotations (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    annotation_id INTEGER NOT NULL CHECK(annotation_id > 0),
    workspace_id TEXT NOT NULL,
    path TEXT NOT NULL,
    file_revision TEXT NOT NULL,
    start_line INTEGER NOT NULL CHECK(start_line > 0),
    end_line INTEGER NOT NULL CHECK(end_line >= start_line),
    body TEXT NOT NULL,
    preview_start_line INTEGER NOT NULL CHECK(preview_start_line > 0),
    preview_end_line INTEGER NOT NULL CHECK(preview_end_line >= preview_start_line),
    preview_text TEXT NOT NULL,
    preview_truncated INTEGER NOT NULL DEFAULT 0 CHECK(preview_truncated IN (0, 1)),
    submission_id TEXT,
    submitted_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK ((submission_id IS NULL) = (submitted_at IS NULL)),
    PRIMARY KEY(session_id, annotation_id)
);

CREATE INDEX annotations_session_order
    ON annotations(session_id, annotation_id);
CREATE INDEX annotations_submission
    ON annotations(session_id, submission_id) WHERE submission_id IS NOT NULL;
