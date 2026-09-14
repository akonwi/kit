package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/peer"
)

func (s *Store) AdmitPeerQuery(ctx context.Context, input peer.Admission, limits peer.Limits) (peer.Request, bool, error) {
	if s == nil || s.db == nil {
		return peer.Request{}, false, fmt.Errorf("store is closed")
	}
	if err := validatePeerAdmission(input, limits); err != nil {
		return peer.Request{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return peer.Request{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if existing, loadErr := scanPeerRequest(tx.QueryRowContext(ctx, peerSelect+` WHERE sender_session_id = ? AND idempotency_key = ?`, input.SenderSessionID, input.IdempotencyKey)); loadErr == nil {
		existingRoute, _ := json.Marshal(existing.Route)
		requestedRoute, _ := json.Marshal(input.Route)
		if existing.RecipientSessionID != input.RecipientSessionID || existing.Message != input.Message || existing.ThreadID != input.ThreadID || existing.PrecedingRequestID != input.PrecedingRequestID || existing.HopCount != input.HopCount || string(existingRoute) != string(requestedRoute) {
			return peer.Request{}, false, fmt.Errorf("%w: idempotency key was reused", peer.ErrConflict)
		}
		return existing, false, nil
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return peer.Request{}, false, loadErr
	}
	for _, id := range []string{input.SenderSessionID, input.RecipientSessionID} {
		var persistent int
		var archived sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT persistent, archived_at FROM sessions WHERE id = ?`, id).Scan(&persistent, &archived); errors.Is(err, sql.ErrNoRows) {
			return peer.Request{}, false, fmt.Errorf("%w: session %q", peer.ErrUnavailable, id)
		} else if err != nil {
			return peer.Request{}, false, err
		} else if persistent != 1 || archived.Valid {
			return peer.Request{}, false, fmt.Errorf("%w: session %q is not eligible", peer.ErrUnavailable, id)
		}
	}
	if input.PrecedingRequestID != "" {
		var owner, precedingThread string
		if err := tx.QueryRowContext(ctx, `SELECT sender_session_id,COALESCE(thread_id,'') FROM peer_session_queries WHERE id=?`, input.PrecedingRequestID).Scan(&owner, &precedingThread); errors.Is(err, sql.ErrNoRows) || owner != input.SenderSessionID {
			return peer.Request{}, false, peer.ErrNotFound
		} else if err != nil {
			return peer.Request{}, false, err
		}
		if input.ThreadID == "" || precedingThread != input.ThreadID {
			return peer.Request{}, false, fmt.Errorf("%w: thread mismatch", peer.ErrInvalid)
		}
		var depth int
		if err := tx.QueryRowContext(ctx, `WITH RECURSIVE chain(id,preceding_request_id,depth) AS (
			SELECT id,preceding_request_id,1 FROM peer_session_queries WHERE id=?
			UNION ALL SELECT q.id,q.preceding_request_id,chain.depth+1 FROM peer_session_queries q JOIN chain ON q.id=chain.preceding_request_id
		) SELECT COALESCE(MAX(depth),0) FROM chain`, input.PrecedingRequestID).Scan(&depth); err != nil {
			return peer.Request{}, false, err
		}
		if depth >= limits.MaxThreadDepth {
			return peer.Request{}, false, fmt.Errorf("%w: thread depth", peer.ErrInvalid)
		}
	}
	var globalQueued, senderQueued, recipientQueued int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM peer_session_queries WHERE state IN ('queued','processing')`).Scan(&globalQueued); err != nil {
		return peer.Request{}, false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM peer_session_queries WHERE sender_session_id = ? AND state IN ('queued','processing')`, input.SenderSessionID).Scan(&senderQueued); err != nil {
		return peer.Request{}, false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM peer_session_queries WHERE recipient_session_id = ? AND state IN ('queued','processing')`, input.RecipientSessionID).Scan(&recipientQueued); err != nil {
		return peer.Request{}, false, err
	}
	if globalQueued >= limits.MaxQueuedGlobal || senderQueued >= limits.MaxQueuedPerSender || recipientQueued >= limits.MaxQueuedPerRecipient {
		return peer.Request{}, false, peer.ErrQueueFull
	}
	route, _ := json.Marshal(input.Route)
	_, err = tx.ExecContext(ctx, `INSERT INTO peer_session_queries(
		id,idempotency_key,sender_session_id,recipient_session_id,message,thread_id,preceding_request_id,route_json,hop_count,state,generation,created_at
	) VALUES(?,?,?,?,?,NULLIF(?,''),NULLIF(?,''),?,?,'queued',1,?)`, input.ID, input.IdempotencyKey, input.SenderSessionID, input.RecipientSessionID, input.Message, input.ThreadID, input.PrecedingRequestID, string(route), input.HopCount, formatTimestamp(input.Now))
	if err != nil {
		return peer.Request{}, false, fmt.Errorf("admit peer query: %w", err)
	}
	value, err := scanPeerRequest(tx.QueryRowContext(ctx, peerSelect+` WHERE id = ?`, input.ID))
	if err != nil {
		return peer.Request{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return peer.Request{}, false, err
	}
	return value, true, nil
}

func validatePeerAdmission(input peer.Admission, limits peer.Limits) error {
	if !identifier.Valid(input.ID, "peer_") || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 512 || input.SenderSessionID == input.RecipientSessionID || !identifier.Valid(input.SenderSessionID, "session_") || !identifier.Valid(input.RecipientSessionID, "session_") {
		return peer.ErrInvalid
	}
	if strings.TrimSpace(input.Message) == "" || len(input.Message) > limits.MaxMessageBytes || !utf8.ValidString(input.Message) || strings.ContainsRune(input.Message, 0) {
		return fmt.Errorf("%w: message", peer.ErrInvalid)
	}
	if input.HopCount < 0 || input.HopCount > limits.MaxHopCount || len(input.Route) > limits.MaxHopCount+1 {
		return fmt.Errorf("%w: route", peer.ErrInvalid)
	}
	seen := make(map[string]struct{}, len(input.Route))
	for _, id := range input.Route {
		if !identifier.Valid(id, "session_") {
			return fmt.Errorf("%w: route", peer.ErrInvalid)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("%w: route cycle", peer.ErrInvalid)
		}
		seen[id] = struct{}{}
	}
	if len(input.Route) != input.HopCount+1 || len(input.Route) < 2 || input.Route[0] != input.SenderSessionID && input.Route[len(input.Route)-2] != input.SenderSessionID || input.Route[len(input.Route)-1] != input.RecipientSessionID {
		return fmt.Errorf("%w: non-canonical route", peer.ErrInvalid)
	}
	if input.ThreadID != "" && (len(input.ThreadID) > 256 || !utf8.ValidString(input.ThreadID) || strings.ContainsRune(input.ThreadID, 0)) {
		return fmt.Errorf("%w: thread", peer.ErrInvalid)
	}
	if input.PrecedingRequestID != "" && !identifier.Valid(input.PrecedingRequestID, "peer_") {
		return fmt.Errorf("%w: preceding request", peer.ErrInvalid)
	}
	if input.Now.IsZero() {
		return fmt.Errorf("%w: timestamp", peer.ErrInvalid)
	}
	return nil
}

const peerSelect = `SELECT id,idempotency_key,sender_session_id,recipient_session_id,message,COALESCE(thread_id,''),COALESCE(preceding_request_id,''),route_json,hop_count,state,COALESCE(recipient_turn_id,''),COALESCE(result,''),COALESCE(terminal_error,''),generation,created_at,started_at,completed_at FROM peer_session_queries`

func (s *Store) PeerQuery(ctx context.Context, id string) (peer.Request, error) {
	value, err := scanPeerRequest(s.db.QueryRowContext(ctx, peerSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return peer.Request{}, peer.ErrNotFound
	}
	return value, err
}

func (s *Store) ClaimNextPeerQuery(ctx context.Context, recipient string, now time.Time) (peer.Request, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return peer.Request{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var archived sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT archived_at FROM sessions WHERE id = ?`, recipient).Scan(&archived); errors.Is(err, sql.ErrNoRows) {
		return peer.Request{}, peer.ErrUnavailable
	} else if err != nil {
		return peer.Request{}, err
	}
	if archived.Valid {
		_, err = tx.ExecContext(ctx, `UPDATE peer_session_queries SET state='recipient_archived',terminal_error='recipient session was archived',generation=generation+1,completed_at=? WHERE recipient_session_id=? AND state='queued'`, formatTimestamp(now), recipient)
		if err == nil {
			err = tx.Commit()
		}
		return peer.Request{}, errors.Join(peer.ErrUnavailable, err)
	}
	value, err := scanPeerRequest(tx.QueryRowContext(ctx, peerSelect+` WHERE recipient_session_id=? AND state='processing' ORDER BY started_at,id LIMIT 1`, recipient))
	if err == nil {
		if err = tx.Commit(); err != nil {
			return peer.Request{}, err
		}
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return peer.Request{}, err
	}
	value, err = scanPeerRequest(tx.QueryRowContext(ctx, peerSelect+` WHERE recipient_session_id=? AND state='queued' ORDER BY created_at,id LIMIT 1`, recipient))
	if errors.Is(err, sql.ErrNoRows) {
		return peer.Request{}, peer.ErrNotFound
	}
	if err != nil {
		return peer.Request{}, err
	}
	value, err = scanPeerRequest(tx.QueryRowContext(ctx, peerSelect+` WHERE id=? AND state='queued' AND generation=?`, value.ID, value.Generation))
	if err != nil {
		return peer.Request{}, peer.ErrConflict
	}
	res, err := tx.ExecContext(ctx, `UPDATE peer_session_queries SET state='processing',started_at=?,generation=generation+1 WHERE id=? AND state='queued' AND generation=?`, formatTimestamp(now), value.ID, value.Generation)
	if err != nil {
		return peer.Request{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return peer.Request{}, peer.ErrConflict
	}
	value, err = scanPeerRequest(tx.QueryRowContext(ctx, peerSelect+` WHERE id=?`, value.ID))
	if err == nil {
		err = tx.Commit()
	}
	return value, err
}

func (s *Store) BindPeerRecipientTurn(ctx context.Context, id string, generation uint64, turn string) (peer.Request, error) {
	if current, err := s.PeerQuery(ctx, id); err == nil && current.State == peer.StateProcessing && current.RecipientTurnID == turn {
		return current, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE peer_session_queries SET recipient_turn_id=?,generation=generation+1 WHERE id=? AND state='processing' AND generation=? AND recipient_turn_id IS NULL`, turn, id, generation)
	if err != nil {
		return peer.Request{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return peer.Request{}, peer.ErrConflict
	}
	return s.PeerQuery(ctx, id)
}

func (s *Store) CompletePeerQuery(ctx context.Context, c peer.Completion, limits peer.Limits) (peer.Request, error) {
	if !c.State.Terminal() || c.FinishedAt.IsZero() {
		return peer.Request{}, peer.ErrInvalid
	}
	c.Result = boundPeerText(c.Result, limits.MaxResultBytes)
	c.Error = boundPeerText(c.Error, limits.MaxResultBytes)
	res, err := s.db.ExecContext(ctx, `UPDATE peer_session_queries SET state=?,result=NULLIF(?,''),terminal_error=NULLIF(?,''),completed_at=?,generation=generation+1 WHERE id=? AND state='processing' AND generation=? AND (?='' OR recipient_turn_id=?)`, c.State, c.Result, c.Error, formatTimestamp(c.FinishedAt), c.ID, c.Generation, c.RecipientTurnID, c.RecipientTurnID)
	if err != nil {
		return peer.Request{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		current, e := s.PeerQuery(ctx, c.ID)
		if e == nil && current.State.Terminal() {
			return current, nil
		}
		return peer.Request{}, peer.ErrConflict
	}
	value, err := s.PeerQuery(ctx, c.ID)
	if err != nil {
		return peer.Request{}, err
	}
	if limits.MaxTerminalRetained > 0 {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM peer_session_queries WHERE id IN (
			SELECT id FROM peer_session_queries WHERE completed_at IS NOT NULL ORDER BY completed_at DESC,id DESC LIMIT -1 OFFSET ?
		)`, limits.MaxTerminalRetained)
	}
	return value, nil
}

func boundPeerText(value string, max int) string {
	if len(value) <= max {
		return value
	}
	end := max - len("…")
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + "…"
}

func (s *Store) PendingPeerRecipients(ctx context.Context, after string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT recipient_session_id FROM peer_session_queries WHERE state IN ('queued','processing') GROUP BY recipient_session_id ORDER BY MIN(created_at),recipient_session_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) RecoverPeerQueries(ctx context.Context, now time.Time, limits peer.Limits) ([]peer.Request, error) {
	rows, err := s.db.QueryContext(ctx, peerSelect+` WHERE state='processing' ORDER BY started_at,id`)
	if err != nil {
		return nil, err
	}
	var values []peer.Request
	for rows.Next() {
		v, e := scanPeerRequest(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		values = append(values, v)
	}
	rows.Close()
	return values, nil
}

func (s *Store) TerminalPeerResults(ctx context.Context, limit int) ([]peer.Request, error) {
	rows, err := s.db.QueryContext(ctx, peerSelect+` WHERE completed_at IS NOT NULL ORDER BY completed_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []peer.Request
	for rows.Next() {
		value, err := scanPeerRequest(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) PeerQueryByRecipientTurn(ctx context.Context, recipient, turn string) (peer.Request, error) {
	v, e := scanPeerRequest(s.db.QueryRowContext(ctx, peerSelect+` WHERE recipient_session_id=? AND recipient_turn_id=?`, recipient, turn))
	if errors.Is(e, sql.ErrNoRows) {
		e = peer.ErrNotFound
	}
	return v, e
}

func scanPeerRequest(scanner rowScanner) (peer.Request, error) {
	var v peer.Request
	var route string
	var started, completed sql.NullString
	var created string
	err := scanner.Scan(&v.ID, &v.IdempotencyKey, &v.SenderSessionID, &v.RecipientSessionID, &v.Message, &v.ThreadID, &v.PrecedingRequestID, &route, &v.HopCount, &v.State, &v.RecipientTurnID, &v.Result, &v.Error, &v.Generation, &created, &started, &completed)
	if err != nil {
		return peer.Request{}, err
	}
	if err = json.Unmarshal([]byte(route), &v.Route); err != nil {
		return peer.Request{}, err
	}
	var e error
	v.CreatedAt, e = parseTimestamp(created)
	if e != nil {
		return peer.Request{}, e
	}
	if started.Valid {
		t, e := parseTimestamp(started.String)
		if e != nil {
			return peer.Request{}, e
		}
		v.StartedAt = &t
	}
	if completed.Valid {
		t, e := parseTimestamp(completed.String)
		if e != nil {
			return peer.Request{}, e
		}
		v.CompletedAt = &t
	}
	return v, nil
}
