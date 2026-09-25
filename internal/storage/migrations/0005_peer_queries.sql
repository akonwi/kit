CREATE TABLE peer_session_queries (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL,
    sender_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    recipient_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    message TEXT NOT NULL,
    thread_id TEXT,
    preceding_request_id TEXT REFERENCES peer_session_queries(id) ON DELETE SET NULL,
    route_json TEXT NOT NULL DEFAULT '[]',
    hop_count INTEGER NOT NULL DEFAULT 0 CHECK (hop_count >= 0),
    state TEXT NOT NULL CHECK (state IN (
        'queued', 'processing', 'completed', 'failed', 'aborted', 'interrupted',
        'recipient_archived', 'recipient_unavailable'
    )),
    recipient_turn_id TEXT,
    result TEXT,
    terminal_error TEXT,
    generation INTEGER NOT NULL DEFAULT 1 CHECK (generation > 0),
    created_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT,
    UNIQUE(sender_session_id, idempotency_key),
    CHECK(sender_session_id <> recipient_session_id)
);

CREATE INDEX peer_queries_recipient_queue_idx
    ON peer_session_queries(recipient_session_id, state, created_at, id);
CREATE UNIQUE INDEX peer_queries_active_recipient_idx
    ON peer_session_queries(recipient_session_id)
    WHERE state = 'processing';
CREATE UNIQUE INDEX peer_queries_recipient_turn_idx
    ON peer_session_queries(recipient_session_id, recipient_turn_id)
    WHERE recipient_turn_id IS NOT NULL;
CREATE INDEX peer_queries_sender_activity_idx
    ON peer_session_queries(sender_session_id, created_at DESC, id DESC);
CREATE INDEX peer_queries_processing_idx
    ON peer_session_queries(state, started_at)
    WHERE state = 'processing';
