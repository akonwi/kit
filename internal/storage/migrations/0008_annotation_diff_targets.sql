ALTER TABLE annotations ADD COLUMN target_workspace_id TEXT;
ALTER TABLE annotations ADD COLUMN target_kind TEXT CHECK(target_kind IN ('commit', 'branch'));
ALTER TABLE annotations ADD COLUMN target_base_kind TEXT CHECK(target_base_kind IN ('empty_tree', 'commit'));
ALTER TABLE annotations ADD COLUMN target_base_oid TEXT;
ALTER TABLE annotations ADD COLUMN target_head_kind TEXT CHECK(target_head_kind = 'commit');
ALTER TABLE annotations ADD COLUMN target_head_oid TEXT;

CREATE TRIGGER annotations_diff_target_insert
BEFORE INSERT ON annotations
WHEN NOT (
    (NEW.target_workspace_id IS NULL AND NEW.target_kind IS NULL AND NEW.target_base_kind IS NULL AND NEW.target_base_oid IS NULL AND NEW.target_head_kind IS NULL AND NEW.target_head_oid IS NULL)
    OR
    (NEW.anchor_kind = 'working_tree_diff' AND NEW.target_workspace_id IS NOT NULL AND NEW.target_kind IS NOT NULL
     AND NEW.target_base_kind IS NOT NULL AND NEW.target_head_kind = 'commit' AND NEW.target_head_oid IS NOT NULL
     AND ((NEW.target_base_kind = 'empty_tree' AND NEW.target_base_oid IS NULL)
          OR (NEW.target_base_kind = 'commit' AND NEW.target_base_oid IS NOT NULL)))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid annotation diff target');
END;

CREATE TRIGGER annotations_diff_target_update
BEFORE UPDATE OF anchor_kind, target_workspace_id, target_kind, target_base_kind, target_base_oid, target_head_kind, target_head_oid ON annotations
WHEN NOT (
    (NEW.target_workspace_id IS NULL AND NEW.target_kind IS NULL AND NEW.target_base_kind IS NULL AND NEW.target_base_oid IS NULL AND NEW.target_head_kind IS NULL AND NEW.target_head_oid IS NULL)
    OR
    (NEW.anchor_kind = 'working_tree_diff' AND NEW.target_workspace_id IS NOT NULL AND NEW.target_kind IS NOT NULL
     AND NEW.target_base_kind IS NOT NULL AND NEW.target_head_kind = 'commit' AND NEW.target_head_oid IS NOT NULL
     AND ((NEW.target_base_kind = 'empty_tree' AND NEW.target_base_oid IS NULL)
          OR (NEW.target_base_kind = 'commit' AND NEW.target_base_oid IS NOT NULL)))
)
BEGIN
    SELECT RAISE(ABORT, 'invalid annotation diff target');
END;
