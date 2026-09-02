CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    cwd TEXT NOT NULL,
    name TEXT,
    persistent INTEGER NOT NULL DEFAULT 1 CHECK (persistent IN (0, 1)),
    parent_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    model_provider TEXT,
    model_id TEXT,
    thinking_level TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    archived_at TEXT,
    CHECK (parent_session_id IS NULL OR parent_session_id <> id)
);

CREATE INDEX sessions_cwd_updated_idx
    ON sessions(cwd, updated_at DESC);
CREATE INDEX sessions_parent_idx
    ON sessions(parent_session_id)
    WHERE parent_session_id IS NOT NULL;

CREATE TRIGGER sessions_reject_parent_cycle_on_insert
BEFORE INSERT ON sessions
WHEN NEW.parent_session_id IS NOT NULL
BEGIN
    SELECT CASE WHEN EXISTS (
        WITH RECURSIVE ancestors(id) AS (
            SELECT NEW.parent_session_id
            UNION
            SELECT sessions.parent_session_id
            FROM sessions
            JOIN ancestors ON sessions.id = ancestors.id
            WHERE sessions.parent_session_id IS NOT NULL
        )
        SELECT 1 FROM ancestors WHERE id = NEW.id
    ) THEN RAISE(ABORT, 'session parent cycle') END;
END;

CREATE TRIGGER sessions_reject_parent_cycle_on_update
BEFORE UPDATE OF parent_session_id ON sessions
WHEN NEW.parent_session_id IS NOT NULL
BEGIN
    SELECT CASE WHEN EXISTS (
        WITH RECURSIVE ancestors(id) AS (
            SELECT NEW.parent_session_id
            UNION
            SELECT sessions.parent_session_id
            FROM sessions
            JOIN ancestors ON sessions.id = ancestors.id
            WHERE sessions.parent_session_id IS NOT NULL
        )
        SELECT 1 FROM ancestors WHERE id = NEW.id
    ) THEN RAISE(ABORT, 'session parent cycle') END;
END;

CREATE TABLE turns (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'aborted', 'interrupted')),
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (session_id, sequence),
    UNIQUE (session_id, id)
);

CREATE INDEX turns_session_created_idx
    ON turns(session_id, created_at);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn_id TEXT,
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool', 'system', 'bash')),
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (session_id, sequence),
    FOREIGN KEY (session_id, turn_id) REFERENCES turns(session_id, id)
);

CREATE INDEX messages_session_turn_idx
    ON messages(session_id, turn_id, sequence);

CREATE TABLE parent_runs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'aborted', 'interrupted')),
    error TEXT,
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (session_id, id),
    FOREIGN KEY (session_id, turn_id) REFERENCES turns(session_id, id)
);

CREATE UNIQUE INDEX parent_runs_one_active_idx
    ON parent_runs(session_id)
    WHERE status = 'running';

CREATE TABLE subagent_conversations (
    id TEXT PRIMARY KEY,
    parent_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL,
    model_provider TEXT,
    model_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('idle', 'running', 'completed', 'failed', 'aborted', 'interrupted', 'dismissed')),
    last_activity_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    dismissed_at TEXT,
    UNIQUE (parent_session_id, id)
);

CREATE UNIQUE INDEX subagent_conversations_active_agent_idx
    ON subagent_conversations(parent_session_id, agent_name)
    WHERE dismissed_at IS NULL;

CREATE TABLE subagent_runs (
    id TEXT PRIMARY KEY,
    parent_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    conversation_id TEXT NOT NULL,
    parent_run_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'aborted', 'interrupted')),
    prompt_json TEXT NOT NULL,
    result_json TEXT,
    error TEXT,
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (parent_session_id, id),
    UNIQUE (conversation_id, id),
    FOREIGN KEY (parent_session_id, conversation_id) REFERENCES subagent_conversations(parent_session_id, id) ON DELETE CASCADE,
    FOREIGN KEY (parent_session_id, parent_run_id) REFERENCES parent_runs(session_id, id)
);

CREATE UNIQUE INDEX subagent_runs_one_active_idx
    ON subagent_runs(conversation_id)
    WHERE status = 'running';

CREATE TABLE subagent_messages (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES subagent_conversations(id) ON DELETE CASCADE,
    run_id TEXT,
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool', 'system')),
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (conversation_id, sequence),
    FOREIGN KEY (conversation_id, run_id) REFERENCES subagent_runs(conversation_id, id)
);

CREATE TABLE session_mailbox (
    id TEXT PRIMARY KEY,
    parent_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    subagent_run_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('subagent.completed', 'subagent.failed', 'subagent.aborted', 'subagent.interrupted')),
    payload_json TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'injected', 'dismissed')),
    created_at TEXT NOT NULL,
    injected_at TEXT,
    UNIQUE (subagent_run_id, kind),
    FOREIGN KEY (parent_session_id, subagent_run_id) REFERENCES subagent_runs(parent_session_id, id) ON DELETE CASCADE
);

CREATE INDEX session_mailbox_pending_idx
    ON session_mailbox(parent_session_id, created_at)
    WHERE state = 'pending';

CREATE TABLE session_streams (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    stream_id TEXT NOT NULL,
    next_sequence INTEGER NOT NULL DEFAULT 1 CHECK (next_sequence >= 1),
    updated_at TEXT NOT NULL
);

CREATE TABLE session_events (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    stream_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (session_id, stream_id, sequence)
);

CREATE INDEX session_events_retention_idx
    ON session_events(session_id, stream_id, created_at);
