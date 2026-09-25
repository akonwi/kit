package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/protocol"
)

// CreateAnnotation allocates and inserts one session annotation atomically.
func (s *Store) CreateAnnotation(ctx context.Context, record kitannotation.Record, limit int) (kitannotation.Record, error) {
	if s == nil || s.db == nil {
		return kitannotation.Record{}, fmt.Errorf("store is closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return kitannotation.Record{}, fmt.Errorf("begin annotation creation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM annotations WHERE session_id = ?`, record.SessionID).Scan(&count); err != nil {
		return kitannotation.Record{}, fmt.Errorf("count annotations: %w", err)
	}
	if count >= limit {
		return kitannotation.Record{}, kitannotation.ErrCapacity
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO annotation_sequences(session_id, next_id) VALUES (?, 2)
		ON CONFLICT(session_id) DO UPDATE SET next_id = next_id + 1
		RETURNING next_id - 1
	`, record.SessionID).Scan(&id); err != nil {
		return kitannotation.Record{}, fmt.Errorf("allocate annotation id: %w", err)
	}
	now := formatTimestamp(time.Now().UTC())
	truncated := 0
	if record.Preview.Truncated {
		truncated = 1
	}
	kind, workspaceID, targetID, targetRevision, path, fileRevision, side, startLine, endLine, err := annotationAnchorColumns(record.Anchor)
	if err != nil {
		return kitannotation.Record{}, err
	}
	targetWorkspaceID, targetKind, targetBaseKind, targetBaseOID, targetHeadKind, targetHeadOID, err := annotationTargetColumns(record.DiffTarget)
	if err != nil {
		return kitannotation.Record{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO annotations(
			session_id, annotation_id, anchor_kind, workspace_id, target_id, target_revision, path, file_revision, side,
			start_line, end_line, body, preview_start_line, preview_end_line,
			preview_text, preview_truncated, target_workspace_id, target_kind, target_base_kind,
			target_base_oid, target_head_kind, target_head_oid, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.SessionID, id, kind, workspaceID, targetID, targetRevision, path, fileRevision, side,
		startLine, endLine, record.Body, record.Preview.StartLine, record.Preview.EndLine,
		record.Preview.Text, truncated, targetWorkspaceID, targetKind, targetBaseKind, targetBaseOID, targetHeadKind, targetHeadOID, now, now)
	if err != nil {
		return kitannotation.Record{}, fmt.Errorf("insert annotation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return kitannotation.Record{}, fmt.Errorf("commit annotation creation: %w", err)
	}
	record.ID = uint64(id)
	return record, nil
}

// GetAnnotation loads one live annotation.
func (s *Store) GetAnnotation(ctx context.Context, sessionID string, id uint64) (kitannotation.Record, error) {
	if s == nil || s.db == nil {
		return kitannotation.Record{}, fmt.Errorf("store is closed")
	}
	record, err := scanAnnotation(s.db.QueryRowContext(ctx, annotationSelect+` WHERE session_id = ? AND annotation_id = ? AND submission_id IS NULL`, sessionID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return kitannotation.Record{}, kitannotation.ErrNotFound
	}
	if err != nil {
		return kitannotation.Record{}, fmt.Errorf("get annotation: %w", err)
	}
	return record, nil
}

// ListAnnotations loads live annotations after an exclusive ID in ascending order.
func (s *Store) ListAnnotations(ctx context.Context, sessionID string, after uint64, limit int) ([]kitannotation.Record, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	rows, err := s.db.QueryContext(ctx, annotationSelect+` WHERE session_id = ? AND annotation_id > ? AND submission_id IS NULL ORDER BY annotation_id LIMIT ?`, sessionID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}
	defer rows.Close()
	records := make([]kitannotation.Record, 0)
	for rows.Next() {
		record, scanErr := scanAnnotation(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan annotation: %w", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}
	return records, nil
}

// UpdateAnnotationBody replaces one live annotation body.
func (s *Store) UpdateAnnotationBody(ctx context.Context, sessionID string, id uint64, body string) (kitannotation.Record, error) {
	if s == nil || s.db == nil {
		return kitannotation.Record{}, fmt.Errorf("store is closed")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE annotations SET body = ?, updated_at = ? WHERE session_id = ? AND annotation_id = ? AND submission_id IS NULL`, body, formatTimestamp(time.Now().UTC()), sessionID, id)
	if err != nil {
		return kitannotation.Record{}, fmt.Errorf("update annotation: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return kitannotation.Record{}, fmt.Errorf("count updated annotations: %w", err)
	}
	if changed == 0 {
		return kitannotation.Record{}, kitannotation.ErrNotFound
	}
	return s.GetAnnotation(ctx, sessionID, id)
}

// ReserveAnnotations atomically hides an exact set of live annotations under
// an idempotent submission identity.
func (s *Store) ReserveAnnotations(ctx context.Context, sessionID, submissionID string, ids []uint64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	arguments := make([]any, 0, len(ids)+1)
	arguments = append(arguments, sessionID)
	for _, id := range ids {
		arguments = append(arguments, id)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin annotation reservation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM annotations WHERE session_id = ? AND submission_id IS NULL AND annotation_id IN (`+placeholders+`)`, arguments...).Scan(&count); err != nil {
		return fmt.Errorf("count reserved annotations: %w", err)
	}
	if count != len(ids) {
		return kitannotation.ErrNotFound
	}
	updateArguments := []any{submissionID, formatTimestamp(time.Now().UTC())}
	updateArguments = append(updateArguments, arguments...)
	if _, err := tx.ExecContext(ctx, `UPDATE annotations SET submission_id = ?, submitted_at = ? WHERE session_id = ? AND submission_id IS NULL AND annotation_id IN (`+placeholders+`)`, updateArguments...); err != nil {
		return fmt.Errorf("reserve annotations: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit annotation reservation: %w", err)
	}
	return nil
}

// FinalizeAnnotationSubmission permanently removes a reserved submission.
func (s *Store) FinalizeAnnotationSubmission(ctx context.Context, sessionID, submissionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM annotations WHERE session_id = ? AND submission_id = ?`, sessionID, submissionID)
	if err != nil {
		return fmt.Errorf("finalize annotation submission: %w", err)
	}
	return nil
}

// RollbackAnnotationSubmission makes a reserved submission live again.
func (s *Store) RollbackAnnotationSubmission(ctx context.Context, sessionID, submissionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE annotations SET submission_id = NULL, submitted_at = NULL WHERE session_id = ? AND submission_id = ?`, sessionID, submissionID)
	if err != nil {
		return fmt.Errorf("rollback annotation submission: %w", err)
	}
	return nil
}

// PendingAnnotationSubmissions lists durable reservation identities.
func (s *Store) PendingAnnotationSubmissions(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT submission_id FROM annotations WHERE session_id = ? AND submission_id IS NOT NULL ORDER BY submitted_at`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list annotation submissions: %w", err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

// DeleteAnnotation permanently removes one live annotation.
func (s *Store) DeleteAnnotation(ctx context.Context, sessionID string, id uint64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM annotations WHERE session_id = ? AND annotation_id = ? AND submission_id IS NULL`, sessionID, id)
	if err != nil {
		return fmt.Errorf("delete annotation: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted annotations: %w", err)
	}
	if changed == 0 {
		return kitannotation.ErrNotFound
	}
	return nil
}

const annotationSelect = `
	SELECT session_id, annotation_id, anchor_kind, workspace_id, target_id, target_revision, path, file_revision, side,
	       start_line, end_line, body, preview_start_line, preview_end_line,
	       preview_text, preview_truncated, target_workspace_id, target_kind, target_base_kind,
	       target_base_oid, target_head_kind, target_head_oid
	FROM annotations`

type annotationScanner interface{ Scan(...any) error }

func scanAnnotation(scanner annotationScanner) (kitannotation.Record, error) {
	var record kitannotation.Record
	var id int64
	var truncated int
	var kind string
	var workspaceID, targetID, targetRevision, side sql.NullString
	var targetWorkspaceID, targetKind, targetBaseKind, targetBaseOID, targetHeadKind, targetHeadOID sql.NullString
	var path, fileRevision string
	var startLine, endLine int
	err := scanner.Scan(
		&record.SessionID, &id, &kind, &workspaceID, &targetID, &targetRevision, &path, &fileRevision, &side,
		&startLine, &endLine, &record.Body, &record.Preview.StartLine, &record.Preview.EndLine,
		&record.Preview.Text, &truncated, &targetWorkspaceID, &targetKind, &targetBaseKind,
		&targetBaseOID, &targetHeadKind, &targetHeadOID,
	)
	if err != nil {
		return kitannotation.Record{}, err
	}
	if id <= 0 || truncated < 0 || truncated > 1 {
		return kitannotation.Record{}, fmt.Errorf("stored annotation is invalid")
	}
	record.ID = uint64(id)
	record.Preview.Truncated = truncated == 1
	switch protocol.AnnotationAnchorKind(kind) {
	case protocol.AnnotationAnchorWorkspaceFile:
		if !workspaceID.Valid || targetID.Valid || targetRevision.Valid || side.Valid {
			return kitannotation.Record{}, fmt.Errorf("stored workspace annotation anchor is invalid")
		}
		record.Anchor = protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
			WorkspaceID: workspaceID.String, Path: path, FileRevision: fileRevision, StartLine: startLine, EndLine: endLine,
		}}
	case protocol.AnnotationAnchorWorkingTreeDiff:
		if workspaceID.Valid || !targetID.Valid || !targetRevision.Valid || !side.Valid {
			return kitannotation.Record{}, fmt.Errorf("stored diff annotation anchor is invalid")
		}
		record.Anchor = protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &protocol.WorkingTreeDiffAnnotationAnchor{
			TargetID: targetID.String, TargetRevision: targetRevision.String, Path: path, FileRevision: fileRevision,
			Side: side.String, StartLine: startLine, EndLine: endLine,
		}}
	default:
		return kitannotation.Record{}, fmt.Errorf("stored annotation anchor kind is invalid")
	}
	if targetWorkspaceID.Valid || targetKind.Valid || targetBaseKind.Valid || targetBaseOID.Valid || targetHeadKind.Valid || targetHeadOID.Valid {
		if !targetWorkspaceID.Valid || !targetKind.Valid || !targetBaseKind.Valid || !targetHeadKind.Valid || !targetHeadOID.Valid {
			return kitannotation.Record{}, fmt.Errorf("stored diff target definition is incomplete")
		}
		record.DiffTarget = &protocol.PinnedDiffTarget{
			WorkspaceID: targetWorkspaceID.String, Kind: targetKind.String,
			Base: protocol.DiffEndpoint{Kind: targetBaseKind.String, OID: targetBaseOID.String},
			Head: protocol.DiffEndpoint{Kind: targetHeadKind.String, OID: targetHeadOID.String},
		}
		if record.Anchor.WorkingTreeDiff == nil || record.DiffTarget.Validate() != nil {
			return kitannotation.Record{}, fmt.Errorf("stored diff target definition is invalid")
		}
	}
	if record.Anchor.Validate() != nil {
		return kitannotation.Record{}, fmt.Errorf("stored annotation anchor is invalid")
	}
	return record, nil
}

func annotationAnchorColumns(anchor protocol.AnnotationAnchor) (kind string, workspaceID, targetID, targetRevision any, path, fileRevision string, side any, startLine, endLine int, err error) {
	if validationErr := anchor.Validate(); validationErr != nil {
		err = fmt.Errorf("annotation anchor is invalid")
		return
	}
	kind = string(anchor.Kind)
	if value := anchor.WorkspaceFile; value != nil {
		workspaceID, path, fileRevision = value.WorkspaceID, value.Path, value.FileRevision
		startLine, endLine = value.StartLine, value.EndLine
		return
	}
	value := anchor.WorkingTreeDiff
	targetID, targetRevision, path, fileRevision, side = value.TargetID, value.TargetRevision, value.Path, value.FileRevision, value.Side
	startLine, endLine = value.StartLine, value.EndLine
	return
}

func annotationTargetColumns(target *protocol.PinnedDiffTarget) (workspaceID, kind, baseKind, baseOID, headKind, headOID any, err error) {
	if target == nil {
		return nil, nil, nil, nil, nil, nil, nil
	}
	if validationErr := target.Validate(); validationErr != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("annotation diff target is invalid")
	}
	workspaceID, kind, baseKind, headKind, headOID = target.WorkspaceID, target.Kind, target.Base.Kind, target.Head.Kind, target.Head.OID
	if target.Base.OID != "" {
		baseOID = target.Base.OID
	}
	return
}
