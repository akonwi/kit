-- Keep the exact external child stores owned by a deletion job so runtime
-- retries never need a global orphan scan while session creation is active.
CREATE TABLE session_deletion_artifacts (
    session_id TEXT NOT NULL REFERENCES deleted_session_ids(id) ON DELETE CASCADE,
    artifact_kind TEXT NOT NULL CHECK (artifact_kind = 'subagent_store'),
    artifact_id TEXT NOT NULL,
    PRIMARY KEY (session_id, artifact_kind, artifact_id)
);
