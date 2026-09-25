CREATE TABLE subagent_conversations (
    id TEXT PRIMARY KEY,
    owner_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL,
    agent_description TEXT NOT NULL,
    agent_model TEXT,
    agent_instructions TEXT NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('user', 'project', 'plugin')),
    source_path TEXT NOT NULL,
    source_plugin_id TEXT,
    cwd TEXT NOT NULL,
    model TEXT NOT NULL,
    thinking_level TEXT,
    droid_initialized_at TEXT,
    state TEXT NOT NULL CHECK (state IN ('idle', 'running', 'failed', 'aborted', 'interrupted')),
    generation INTEGER NOT NULL DEFAULT 1 CHECK (generation > 0),
    active_task_id TEXT,
    last_completed_task_id TEXT,
    last_result_summary TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    dismissed_at TEXT,
    UNIQUE(id, owner_session_id)
);

CREATE UNIQUE INDEX subagent_conversations_active_agent_idx
    ON subagent_conversations(owner_session_id, agent_name)
    WHERE dismissed_at IS NULL;
CREATE INDEX subagent_conversations_owner_activity_idx
    ON subagent_conversations(owner_session_id, updated_at DESC)
    WHERE dismissed_at IS NULL;

CREATE TABLE subagent_tasks (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    owner_session_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    message TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('queued', 'running', 'completed', 'failed', 'aborted', 'interrupted')),
    priority INTEGER NOT NULL DEFAULT 0,
    retry_of_task_id TEXT REFERENCES subagent_tasks(id) ON DELETE SET NULL,
    child_turn_id TEXT,
    cancellation_generation INTEGER NOT NULL DEFAULT 1 CHECK (cancellation_generation > 0),
    cancellation_requested_at TEXT,
    cancellation_reason TEXT,
    queued_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    result_summary TEXT,
    terminal_error TEXT,
    FOREIGN KEY(conversation_id, owner_session_id)
        REFERENCES subagent_conversations(id, owner_session_id) ON DELETE CASCADE,
    UNIQUE(conversation_id, sequence)
);

CREATE UNIQUE INDEX subagent_tasks_running_conversation_idx
    ON subagent_tasks(conversation_id)
    WHERE state = 'running';
CREATE INDEX subagent_tasks_oldest_session_idx
    ON subagent_tasks(owner_session_id, state, queued_at, id);
CREATE INDEX subagent_tasks_conversation_queue_idx
    ON subagent_tasks(conversation_id, state, sequence);
CREATE INDEX subagent_tasks_active_session_idx
    ON subagent_tasks(owner_session_id, started_at)
    WHERE state = 'running';
CREATE INDEX subagent_tasks_running_startup_idx
    ON subagent_tasks(state, started_at)
    WHERE state = 'running';

CREATE TABLE subagent_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    conversation_id TEXT NOT NULL REFERENCES subagent_conversations(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES subagent_tasks(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    payload TEXT,
    occurred_at TEXT NOT NULL
);

CREATE INDEX subagent_events_conversation_sequence_idx
    ON subagent_events(conversation_id, sequence);
CREATE INDEX subagent_events_owner_sequence_idx
    ON subagent_events(owner_session_id, sequence);

CREATE TABLE parent_mailbox (
    id TEXT PRIMARY KEY,
    owner_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    conversation_id TEXT NOT NULL REFERENCES subagent_conversations(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL UNIQUE REFERENCES subagent_tasks(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL,
    task_state TEXT NOT NULL CHECK (task_state IN ('completed', 'failed', 'aborted', 'interrupted')),
    summary TEXT,
    terminal_error TEXT,
    generation INTEGER NOT NULL DEFAULT 1 CHECK (generation > 0),
    created_at TEXT NOT NULL,
    delivered_at TEXT
);

CREATE INDEX parent_mailbox_pending_idx
    ON parent_mailbox(owner_session_id, created_at, id)
    WHERE delivered_at IS NULL;
