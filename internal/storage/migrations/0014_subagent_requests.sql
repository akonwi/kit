ALTER TABLE subagent_tasks ADD COLUMN origin_kind TEXT NOT NULL DEFAULT 'parent'
    CHECK (origin_kind IN ('parent', 'request', 'reply'));
ALTER TABLE subagent_tasks ADD COLUMN request_id TEXT;

CREATE TABLE subagent_requests (
    id TEXT PRIMARY KEY,
    owner_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    sender_kind TEXT NOT NULL CHECK (sender_kind IN ('parent', 'child')),
    sender_conversation_id TEXT REFERENCES subagent_conversations(id) ON DELETE SET NULL,
    recipient_conversation_id TEXT REFERENCES subagent_conversations(id) ON DELETE SET NULL,
    recipient_name TEXT NOT NULL,
    sender_name TEXT NOT NULL,
    sender_identity TEXT NOT NULL,
    call_identity TEXT NOT NULL,
    delivery_mode TEXT NOT NULL CHECK (delivery_mode IN ('ask', 'send')),
    message TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('open', 'replied', 'failed', 'expired')),
    reply TEXT,
    reply_call_identity TEXT,
    failure TEXT,
    created_at TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    resolved_at TEXT,
    UNIQUE(owner_session_id, sender_identity, call_identity)
);
CREATE INDEX subagent_requests_recipient_idx
    ON subagent_requests(recipient_conversation_id, state, created_at, id)
    WHERE state = 'open';
CREATE INDEX subagent_requests_sender_idx
    ON subagent_requests(owner_session_id, sender_identity, created_at DESC);
CREATE INDEX subagent_requests_deadline_idx
    ON subagent_requests(deadline_at, id) WHERE state = 'open';

CREATE TABLE subagent_request_deliveries (
    request_id TEXT NOT NULL REFERENCES subagent_requests(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('request', 'reply')),
    task_id TEXT UNIQUE REFERENCES subagent_tasks(id) ON DELETE SET NULL,
    delivered_at TEXT,
    PRIMARY KEY(request_id, kind)
);
CREATE INDEX subagent_request_deliveries_pending_idx
    ON subagent_request_deliveries(kind, request_id) WHERE task_id IS NULL;

CREATE TABLE subagent_parent_reply_mailbox (
    request_id TEXT PRIMARY KEY REFERENCES subagent_requests(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    delivered_at TEXT
);
CREATE INDEX subagent_parent_reply_pending_idx
    ON subagent_parent_reply_mailbox(created_at, request_id) WHERE delivered_at IS NULL;
