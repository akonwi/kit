CREATE UNIQUE INDEX messages_one_active_bash_idx
    ON messages(session_id)
    WHERE role = 'bash'
      AND json_extract(payload_json, '$.status') = 'running';
