CREATE TABLE devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    created_at TEXT NOT NULL,
    last_used_at TEXT NOT NULL,
    last_address TEXT NOT NULL DEFAULT '',
    expires_at TEXT NOT NULL
);
CREATE INDEX devices_expires_idx ON devices(expires_at);
