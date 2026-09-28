CREATE TABLE subagent_parent_deliveries (
    id TEXT PRIMARY KEY,
    owner_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('task', 'request')),
    task_id TEXT UNIQUE REFERENCES subagent_tasks(id) ON DELETE CASCADE,
    request_id TEXT UNIQUE REFERENCES subagent_requests(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL,
    task_state TEXT NOT NULL CHECK (task_state IN ('completed', 'failed', 'aborted', 'interrupted')),
    summary TEXT,
    terminal_error TEXT,
    generation INTEGER NOT NULL DEFAULT 1 CHECK (generation > 0),
    created_at TEXT NOT NULL,
    delivered_at TEXT,
    CHECK ((kind = 'task' AND task_id IS NOT NULL AND request_id IS NULL) OR
           (kind = 'request' AND request_id IS NOT NULL AND task_id IS NULL))
);

INSERT INTO subagent_parent_deliveries (
    id, owner_session_id, kind, task_id, agent_name, task_state,
    summary, terminal_error, generation, created_at, delivered_at
)
SELECT id, owner_session_id, 'task', task_id, agent_name, task_state,
       summary, terminal_error, generation, created_at, delivered_at
FROM parent_mailbox;

INSERT INTO subagent_parent_deliveries (
    id, owner_session_id, kind, request_id, agent_name, task_state,
    summary, terminal_error, generation, created_at, delivered_at
)
SELECT 'mail_' || substr(r.id, 12), r.owner_session_id, 'request', r.id,
       r.recipient_name, CASE WHEN r.state = 'replied' THEN 'completed' ELSE 'failed' END,
       r.reply, r.failure, 1, m.created_at, m.delivered_at
FROM subagent_parent_reply_mailbox m JOIN subagent_requests r ON r.id = m.request_id;

CREATE INDEX subagent_parent_deliveries_pending_idx
    ON subagent_parent_deliveries(owner_session_id, created_at, id)
    WHERE delivered_at IS NULL;

DROP TABLE parent_mailbox;
DROP TABLE subagent_parent_reply_mailbox;
