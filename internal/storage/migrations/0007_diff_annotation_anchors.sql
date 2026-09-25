ALTER TABLE annotations RENAME TO annotations_workspace_file_only;

CREATE TABLE annotations (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    annotation_id INTEGER NOT NULL CHECK(annotation_id > 0),
    anchor_kind TEXT NOT NULL CHECK(anchor_kind IN ('workspace_file', 'working_tree_diff')),
    workspace_id TEXT,
    target_id TEXT,
    target_revision TEXT,
    path TEXT NOT NULL,
    file_revision TEXT NOT NULL,
    side TEXT CHECK(side IN ('old', 'new')),
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
    CHECK (
        (anchor_kind = 'workspace_file' AND workspace_id IS NOT NULL AND target_id IS NULL AND target_revision IS NULL AND side IS NULL)
        OR
        (anchor_kind = 'working_tree_diff' AND workspace_id IS NULL AND target_id IS NOT NULL AND target_revision IS NOT NULL AND side IS NOT NULL)
    ),
    PRIMARY KEY(session_id, annotation_id)
);

INSERT INTO annotations(
    session_id, annotation_id, anchor_kind, workspace_id, path, file_revision,
    start_line, end_line, body, preview_start_line, preview_end_line,
    preview_text, preview_truncated, submission_id, submitted_at, created_at, updated_at
)
SELECT
    session_id, annotation_id, 'workspace_file', workspace_id, path, file_revision,
    start_line, end_line, body, preview_start_line, preview_end_line,
    preview_text, preview_truncated, submission_id, submitted_at, created_at, updated_at
FROM annotations_workspace_file_only;

DROP TABLE annotations_workspace_file_only;

CREATE INDEX annotations_session_order
    ON annotations(session_id, annotation_id);
CREATE INDEX annotations_submission
    ON annotations(session_id, submission_id) WHERE submission_id IS NOT NULL;
