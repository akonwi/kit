-- Deleting a scratchpad owner must preserve shared scratchpads for surviving
-- semantic forks. Storage authorizes a transfer only inside the same
-- transaction that copies the scratchpad to its new owner.
CREATE TABLE scratchpad_owner_transfers (
    source_owner_id TEXT NOT NULL,
    target_owner_id TEXT NOT NULL,
    PRIMARY KEY (source_owner_id, target_owner_id)
);

DROP TRIGGER sessions_reject_scratchpad_owner_update;
CREATE TRIGGER sessions_reject_scratchpad_owner_update
BEFORE UPDATE OF scratchpad_owner_id ON sessions
WHEN NEW.scratchpad_owner_id IS NOT OLD.scratchpad_owner_id
 AND NOT EXISTS (
    SELECT 1 FROM scratchpad_owner_transfers AS transfer
    JOIN scratchpads AS scratchpad ON scratchpad.owner_session_id = transfer.target_owner_id
    WHERE transfer.source_owner_id = OLD.scratchpad_owner_id
      AND transfer.target_owner_id = NEW.scratchpad_owner_id
 )
BEGIN
    SELECT RAISE(ABORT, 'scratchpad owner transfer is not authorized');
END;
