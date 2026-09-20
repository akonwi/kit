ALTER TABLE sessions ADD COLUMN scratchpad_owner_id TEXT REFERENCES sessions(id) ON DELETE RESTRICT;

CREATE TEMP TABLE scratchpad_persistence_guard (
    invalid INTEGER NOT NULL CHECK (invalid = 0)
);
INSERT INTO scratchpad_persistence_guard(invalid)
SELECT 1 FROM sessions WHERE persistent <> 1 LIMIT 1;
DROP TABLE scratchpad_persistence_guard;

-- Parent session identity records provenance as well as semantic forks. Existing
-- rows therefore default to independent ownership; a later explicit migration
-- may merge only lineages verified from canonical droid fork metadata.
UPDATE sessions SET scratchpad_owner_id = id;

CREATE TRIGGER sessions_require_scratchpad_owner_on_insert
BEFORE INSERT ON sessions
WHEN NEW.scratchpad_owner_id IS NULL OR NEW.scratchpad_owner_id = ''
BEGIN
    SELECT RAISE(ABORT, 'session scratchpad owner is required');
END;

CREATE TRIGGER sessions_reject_scratchpad_owner_update
BEFORE UPDATE OF scratchpad_owner_id ON sessions
WHEN NEW.scratchpad_owner_id IS NOT OLD.scratchpad_owner_id
BEGIN
    SELECT RAISE(ABORT, 'session scratchpad owner is immutable');
END;

CREATE TABLE scratchpads (
    owner_session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    content TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    updated_at TEXT NOT NULL,
    migration_required INTEGER NOT NULL DEFAULT 0 CHECK (migration_required IN (0, 1))
);

INSERT INTO scratchpads(owner_session_id, content, revision, updated_at)
SELECT id, '', 1, created_at
FROM sessions
WHERE scratchpad_owner_id = id;

CREATE TRIGGER sessions_create_owned_scratchpad
AFTER INSERT ON sessions
WHEN NEW.scratchpad_owner_id = NEW.id
BEGIN
    INSERT INTO scratchpads(owner_session_id, content, revision, updated_at)
    VALUES (NEW.id, '', 1, NEW.updated_at);
END;

CREATE TRIGGER scratchpads_reject_in_use_delete
BEFORE DELETE ON scratchpads
WHEN EXISTS (
    SELECT 1 FROM sessions WHERE scratchpad_owner_id = OLD.owner_session_id
)
BEGIN
    SELECT RAISE(ABORT, 'scratchpad is still referenced by a session');
END;

CREATE INDEX sessions_scratchpad_owner_idx ON sessions(scratchpad_owner_id);
