ALTER TABLE sessions
    ADD COLUMN configuration_revision INTEGER NOT NULL DEFAULT 1
    CHECK (configuration_revision >= 1);
