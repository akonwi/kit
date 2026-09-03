package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RecoveryResult reports stale in-flight state repaired during daemon startup.
type RecoveryResult struct {
	ParentRuns            int64
	Turns                 int64
	SubagentRuns          int64
	SubagentConversations int64
}

// InterruptActiveRuns marks execution owned by a previous daemon instance as
// interrupted. It is transactional and idempotent.
func (s *Store) InterruptActiveRuns(ctx context.Context, reason string) (RecoveryResult, error) {
	if s == nil || s.db == nil {
		return RecoveryResult{}, fmt.Errorf("store is closed")
	}
	if reason == "" {
		reason = "daemon restarted during active execution"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("begin active run recovery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := formatTimestamp(time.Now())
	var recovered RecoveryResult
	result, err := tx.ExecContext(ctx, `
		UPDATE parent_runs
		SET status = 'interrupted', error = ?, completed_at = ?
		WHERE status IN ('queued', 'running')
	`, reason, now)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("interrupt parent runs: %w", err)
	}
	if recovered.ParentRuns, err = rowsAffected(result); err != nil {
		return RecoveryResult{}, err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE turns
		SET status = 'interrupted', completed_at = ?
		WHERE status IN ('pending', 'running')
	`, now)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("interrupt turns: %w", err)
	}
	if recovered.Turns, err = rowsAffected(result); err != nil {
		return RecoveryResult{}, err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE subagent_runs
		SET status = 'interrupted', error = ?, completed_at = ?
		WHERE status IN ('queued', 'running')
	`, reason, now)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("interrupt subagent runs: %w", err)
	}
	if recovered.SubagentRuns, err = rowsAffected(result); err != nil {
		return RecoveryResult{}, err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE subagent_conversations
		SET status = 'interrupted', last_activity_at = ?
		WHERE status = 'running'
	`, now)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("interrupt subagent conversations: %w", err)
	}
	if recovered.SubagentConversations, err = rowsAffected(result); err != nil {
		return RecoveryResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RecoveryResult{}, fmt.Errorf("commit active run recovery: %w", err)
	}
	return recovered, nil
}

func rowsAffected(result sql.Result) (int64, error) {
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("inspect recovery update: %w", err)
	}
	return count, nil
}
