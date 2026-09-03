ALTER TABLE sessions
    ADD COLUMN next_turn_sequence INTEGER NOT NULL DEFAULT 0 CHECK (next_turn_sequence >= 0);

ALTER TABLE sessions
    ADD COLUMN next_message_sequence INTEGER NOT NULL DEFAULT 0 CHECK (next_message_sequence >= 0);

UPDATE sessions
SET next_turn_sequence = COALESCE((
        SELECT MAX(turns.sequence) + 1
        FROM turns
        WHERE turns.session_id = sessions.id
    ), 0),
    next_message_sequence = COALESCE((
        SELECT MAX(messages.sequence) + 1
        FROM messages
        WHERE messages.session_id = sessions.id
    ), 0);
