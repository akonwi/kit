package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/session"
)

const directBashSelect = `
SELECT session_id, execution_id, sequence, command, cwd, status, output, exit_code,
       exclude_from_context, truncated, timed_out, error_message, started_at, completed_at
FROM direct_bash_history`

// AppendBashHistory durably records one settled direct shell execution.
func (s *Store) AppendBashHistory(ctx context.Context, entry session.BashHistoryEntry) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	execution := entry.Execution
	if execution.SessionID == "" || execution.ID == "" || execution.CompletedAt == nil {
		return fmt.Errorf("settled bash execution identity and completion time are required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO direct_bash_history(
			session_id, execution_id, sequence, command, cwd, status, output, exit_code,
			exclude_from_context, truncated, timed_out, error_message, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, execution.SessionID, execution.ID, execution.Sequence, execution.Command, execution.CWD,
		execution.Status, execution.Output, execution.ExitCode, boolToInt(execution.ExcludeFromContext),
		boolToInt(execution.Truncated), boolToInt(execution.TimedOut), execution.ErrorMessage,
		formatTimestamp(execution.StartedAt), formatTimestamp(*execution.CompletedAt))
	if err != nil {
		return fmt.Errorf("append bash history: %w", err)
	}
	return nil
}

// ListBashHistory returns the complete durable direct-bash history in execution order.
func (s *Store) ListBashHistory(ctx context.Context, sessionID string) ([]session.BashExecution, error) {
	return s.queryBashHistory(ctx, directBashSelect+` WHERE session_id = ? ORDER BY sequence`, sessionID)
}

// BashHistoryPage returns one newest-first page of direct-bash history.
func (s *Store) BashHistoryPage(ctx context.Context, sessionID string, before uint64, limit int) ([]session.BashExecution, bool, error) {
	if limit <= 0 {
		limit = protocol.DefaultBashHistoryPageSize
	}
	if limit > protocol.MaxBashHistoryPageSize {
		limit = protocol.MaxBashHistoryPageSize
	}
	query := directBashSelect + ` WHERE session_id = ?`
	arguments := []any{sessionID}
	if before > 0 {
		query += ` AND sequence < ?`
		arguments = append(arguments, before)
	}
	query += ` ORDER BY sequence DESC LIMIT ?`
	// Read one extra row to learn whether older entries remain.
	arguments = append(arguments, limit+1)
	executions, err := s.queryBashHistory(ctx, query, arguments...)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(executions) > limit
	if hasMore {
		executions = executions[:limit]
	}
	return executions, hasMore, nil
}

func (s *Store) queryBashHistory(ctx context.Context, query string, arguments ...any) ([]session.BashExecution, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query bash history: %w", err)
	}
	defer rows.Close()
	var result []session.BashExecution
	for rows.Next() {
		var execution session.BashExecution
		var startedAt, completedAt string
		var exitCode sql.NullInt64
		var exclude, truncated, timedOut int
		if err := rows.Scan(&execution.SessionID, &execution.ID, &execution.Sequence, &execution.Command,
			&execution.CWD, &execution.Status, &execution.Output, &exitCode, &exclude, &truncated, &timedOut,
			&execution.ErrorMessage, &startedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("decode bash history: %w", err)
		}
		execution.ExcludeFromContext, execution.Truncated, execution.TimedOut = exclude != 0, truncated != 0, timedOut != 0
		if exitCode.Valid {
			value := int(exitCode.Int64)
			execution.ExitCode = &value
		}
		execution.StartedAt, err = parseTimestamp(startedAt)
		if err != nil {
			return nil, fmt.Errorf("decode bash start time: %w", err)
		}
		completed, err := parseTimestamp(completedAt)
		if err != nil {
			return nil, fmt.Errorf("decode bash completion time: %w", err)
		}
		execution.CompletedAt = &completed
		result = append(result, execution)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query bash history: %w", err)
	}
	return result, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
