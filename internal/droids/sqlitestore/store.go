// Package sqlitestore provides the CGO-free, one-file-per-droid SQLite Store.
package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const (
	initialMigration = "migrations/0001_initial.sql"
	defaultPageSize  = 100
	maxPageSize      = 1000
)

// Options configures a dedicated SQLite Store.
type Options struct {
	Path        string
	BusyTimeout time.Duration
}

// Store persists exactly one droid conversation in one SQLite database file.
type Store struct {
	db *sql.DB
}

var _ droids.Store = (*Store)(nil)

// Open opens a dedicated droid database and applies embedded migrations.
func Open(ctx context.Context, options Options) (*Store, error) {
	if options.Path == "" {
		return nil, fmt.Errorf("droids sqlite: database path is required")
	}
	path, err := filepath.Abs(options.Path)
	if err != nil {
		return nil, fmt.Errorf("droids sqlite: resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("droids sqlite: create database directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("droids sqlite: create database: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("droids sqlite: protect database: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("droids sqlite: close bootstrap file: %w", err)
	}

	busyTimeout := options.BusyTimeout
	if busyTimeout == 0 {
		busyTimeout = 5 * time.Second
	}
	if busyTimeout < 0 {
		return nil, fmt.Errorf("droids sqlite: busy timeout must not be negative")
	}

	connector, err := sqlite.NewConnector(sqliteDSN(path, busyTimeout))
	if err != nil {
		return nil, fmt.Errorf("droids sqlite: configure connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	fail := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return fail(fmt.Errorf("droids sqlite: ping database: %w", err))
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		return fail(fmt.Errorf("droids sqlite: enable WAL: %w", err))
	}
	if err := migrate(ctx, db); err != nil {
		return fail(err)
	}
	return &Store{db: db}, nil
}

func sqliteDSN(path string, busyTimeout time.Duration) string {
	uri := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := uri.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	query.Add("_pragma", "synchronous(NORMAL)")
	uri.RawQuery = query.Encode()
	return uri.String()
}

// Close closes the Store's connection pool.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Open binds this Store to one conversation and initializes an empty database.
func (s *Store) Open(ctx context.Context, request droids.OpenConversation) (result droids.OpenConversationResult, err error) {
	if request.ID == "" {
		return result, fmt.Errorf("droids sqlite: conversation id is required")
	}
	err = s.immediate(ctx, func(conn *sql.Conn) error {
		state, loadErr := loadState(ctx, conn)
		switch {
		case loadErr == nil:
			if state.ID != request.ID {
				return fmt.Errorf("droids sqlite: database belongs to another conversation")
			}
			result.Conversation = state
			return nil
		case !errors.Is(loadErr, sql.ErrNoRows):
			return loadErr
		}

		now := sqliteTime(time.Now())
		if _, execErr := conn.ExecContext(ctx, `
			INSERT INTO droid_state(
				singleton, conversation_id, revision,
				last_record_sequence, last_event_sequence, created_at, updated_at
			) VALUES (1, ?, 1, 0, 0, ?, ?)
		`, request.ID, now, now); execErr != nil {
			return fmt.Errorf("droids sqlite: initialize state: %w", execErr)
		}

		var lastRecord uint64
		seen := make(map[string]struct{}, len(request.InitialRecords))
		for _, record := range request.InitialRecords {
			key := record.Kind + "\x00" + record.ID
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("droids sqlite: duplicate initial record %s/%s", record.Kind, record.ID)
			}
			seen[key] = struct{}{}
			var insertErr error
			lastRecord, insertErr = insertInitialRecord(ctx, conn, record, lastRecord)
			if insertErr != nil {
				return insertErr
			}
		}

		var lastEvent droids.EventSequence
		for _, event := range request.InitialEvents {
			if err := validateEvent(event); err != nil {
				return err
			}
			lastEvent++
			if err := insertEvent(ctx, conn, event, lastEvent, 1); err != nil {
				return err
			}
		}
		if _, execErr := conn.ExecContext(ctx, `
			UPDATE droid_state
			SET last_record_sequence = ?, last_event_sequence = ?, updated_at = ?
			WHERE singleton = 1
		`, lastRecord, lastEvent, sqliteTime(time.Now())); execErr != nil {
			return fmt.Errorf("droids sqlite: finish state initialization: %w", execErr)
		}
		state, loadErr = loadState(ctx, conn)
		if loadErr != nil {
			return loadErr
		}
		result = droids.OpenConversationResult{Conversation: state, Created: true}
		return nil
	})
	return result, err
}

// Commit atomically applies mutations and appends durable events.
func (s *Store) Commit(ctx context.Context, request droids.CommitRequest) (result droids.CommitResult, err error) {
	err = s.immediate(ctx, func(conn *sql.Conn) error {
		var revision, lastRecord uint64
		var lastEvent droids.EventSequence
		if queryErr := conn.QueryRowContext(ctx, `
			SELECT revision, last_record_sequence, last_event_sequence
			FROM droid_state WHERE singleton = 1
		`).Scan(&revision, &lastRecord, &lastEvent); queryErr != nil {
			if errors.Is(queryErr, sql.ErrNoRows) {
				return fmt.Errorf("droids sqlite: store is not open")
			}
			return fmt.Errorf("droids sqlite: read revision: %w", queryErr)
		}
		if revision != request.ExpectedRevision {
			return droids.ErrConflict
		}
		nextRevision := revision + 1
		for _, mutation := range request.Mutations {
			var mutationErr error
			lastRecord, mutationErr = applyMutation(ctx, conn, mutation, lastRecord, nextRevision)
			if mutationErr != nil {
				return mutationErr
			}
		}
		for _, event := range request.Events {
			if err := validateEvent(event); err != nil {
				return err
			}
			lastEvent++
			if err := insertEvent(ctx, conn, event, lastEvent, nextRevision); err != nil {
				return err
			}
		}
		updated, execErr := conn.ExecContext(ctx, `
			UPDATE droid_state
			SET revision = ?, last_record_sequence = ?, last_event_sequence = ?, updated_at = ?
			WHERE singleton = 1 AND revision = ?
		`, nextRevision, lastRecord, lastEvent, sqliteTime(time.Now()), revision)
		if execErr != nil {
			return fmt.Errorf("droids sqlite: update state: %w", execErr)
		}
		rows, rowsErr := updated.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("droids sqlite: read updated state rows: %w", rowsErr)
		}
		if rows != 1 {
			return droids.ErrConflict
		}
		result = droids.CommitResult{Revision: nextRevision, LastEvent: lastEvent}
		return nil
	})
	return result, err
}

// State reloads bounded runtime state.
func (s *Store) State(ctx context.Context) (state droids.StoredConversation, err error) {
	err = s.read(ctx, func(conn *sql.Conn) error {
		var loadErr error
		state, loadErr = loadState(ctx, conn)
		return loadErr
	})
	return state, err
}

// Records pages immutable history records.
func (s *Store) Records(ctx context.Context, query droids.RecordQuery) (droids.RecordPage, error) {
	limit, err := pageSize(query.Limit)
	if err != nil {
		return droids.RecordPage{}, err
	}
	conn, err := s.connection(ctx)
	if err != nil {
		return droids.RecordPage{}, err
	}
	defer conn.Close()

	statement := `
		SELECT record_kind, record_id, scope, sequence, version, payload
		FROM records
		WHERE scope = 'history' AND sequence > ?`
	args := []any{query.After}
	if query.Before > 0 {
		statement += " AND sequence < ?"
		args = append(args, query.Before)
	}
	if query.Kind != "" {
		statement += " AND record_kind = ?"
		args = append(args, query.Kind)
	}
	if query.Descending {
		statement += " ORDER BY sequence DESC"
	} else {
		statement += " ORDER BY sequence"
	}
	statement += " LIMIT ?"
	args = append(args, limit+1)
	rows, err := conn.QueryContext(ctx, statement, args...)
	if err != nil {
		return droids.RecordPage{}, fmt.Errorf("droids sqlite: query records: %w", err)
	}
	defer rows.Close()
	var records []droids.EncodedRecord
	for rows.Next() {
		record, scanErr := scanRecord(rows)
		if scanErr != nil {
			return droids.RecordPage{}, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return droids.RecordPage{}, fmt.Errorf("droids sqlite: read records: %w", err)
	}
	page := droids.RecordPage{}
	if len(records) > limit {
		page.HasMore = true
		records = records[:limit]
	}
	page.Records = records
	if len(records) > 0 {
		page.Next = records[len(records)-1].Sequence
	}
	return page, nil
}

// Events pages durable outbox events.
func (s *Store) Events(ctx context.Context, query droids.EventQuery) (droids.EventPage, error) {
	limit, err := pageSize(query.Limit)
	if err != nil {
		return droids.EventPage{}, err
	}
	conn, err := s.connection(ctx)
	if err != nil {
		return droids.EventPage{}, err
	}
	defer conn.Close()
	rows, err := conn.QueryContext(ctx, `
		SELECT sequence, revision, kind, version, payload, occurred_at
		FROM events WHERE sequence > ? ORDER BY sequence LIMIT ?
	`, query.After, limit+1)
	if err != nil {
		return droids.EventPage{}, fmt.Errorf("droids sqlite: query events: %w", err)
	}
	defer rows.Close()
	var events []droids.StoredEvent
	for rows.Next() {
		var event droids.StoredEvent
		var occurredAt string
		if err := rows.Scan(&event.Sequence, &event.Revision, &event.Kind, &event.Version, &event.Payload, &occurredAt); err != nil {
			return droids.EventPage{}, fmt.Errorf("droids sqlite: decode event: %w", err)
		}
		event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return droids.EventPage{}, fmt.Errorf("droids sqlite: decode event time: %w", err)
		}
		event.Payload = append([]byte(nil), event.Payload...)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return droids.EventPage{}, fmt.Errorf("droids sqlite: read events: %w", err)
	}
	page := droids.EventPage{}
	if len(events) > limit {
		page.HasMore = true
		events = events[:limit]
	}
	page.Events = events
	if len(events) > 0 {
		page.Next = events[len(events)-1].Sequence
	}
	return page, nil
}

func (s *Store) connection(ctx context.Context) (*sql.Conn, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("droids sqlite: store is closed")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("droids sqlite: acquire connection: %w", err)
	}
	return conn, nil
}

func (s *Store) read(ctx context.Context, fn func(*sql.Conn) error) (err error) {
	conn, err := s.connection(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("droids sqlite: begin read transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if err = fn(conn); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("droids sqlite: commit read transaction: %w", err)
	}
	return nil
}

func (s *Store) immediate(ctx context.Context, fn func(*sql.Conn) error) (err error) {
	conn, err := s.connection(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("droids sqlite: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if err = fn(conn); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("droids sqlite: commit transaction: %w", err)
	}
	return nil
}

func loadState(ctx context.Context, conn *sql.Conn) (droids.StoredConversation, error) {
	var state droids.StoredConversation
	if err := conn.QueryRowContext(ctx, `
		SELECT conversation_id, revision, last_event_sequence
		FROM droid_state WHERE singleton = 1
	`).Scan(&state.ID, &state.Revision, &state.LastEvent); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state, sql.ErrNoRows
		}
		return state, fmt.Errorf("droids sqlite: read state: %w", err)
	}
	rows, err := conn.QueryContext(ctx, `
		SELECT record_kind, record_id, scope, sequence, version, payload
		FROM records WHERE scope = 'runtime' ORDER BY record_kind, record_id
	`)
	if err != nil {
		return state, fmt.Errorf("droids sqlite: query runtime records: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		record, scanErr := scanRecord(rows)
		if scanErr != nil {
			return state, scanErr
		}
		state.RuntimeState = append(state.RuntimeState, record)
	}
	if err := rows.Err(); err != nil {
		return state, fmt.Errorf("droids sqlite: read runtime records: %w", err)
	}
	return state, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanRecord(row rowScanner) (droids.EncodedRecord, error) {
	var record droids.EncodedRecord
	var sequence sql.NullInt64
	if err := row.Scan(&record.Kind, &record.ID, &record.Scope, &sequence, &record.Version, &record.Payload); err != nil {
		return record, fmt.Errorf("droids sqlite: decode record: %w", err)
	}
	if sequence.Valid {
		record.Sequence = uint64(sequence.Int64)
	}
	record.Payload = append([]byte(nil), record.Payload...)
	return record, nil
}

func insertInitialRecord(ctx context.Context, conn *sql.Conn, record droids.EncodedRecord, lastSequence uint64) (uint64, error) {
	if err := validateRecord(record.Kind, record.ID, record.Scope, record.Version, record.Payload); err != nil {
		return lastSequence, err
	}
	var sequence any
	if record.Scope == droids.RecordHistory {
		lastSequence++
		sequence = lastSequence
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO records(
			record_kind, record_id, scope, sequence, version, payload,
			created_revision, updated_revision
		) VALUES (?, ?, ?, ?, ?, ?, 1, 1)
	`, record.Kind, record.ID, record.Scope, sequence, record.Version, []byte(record.Payload)); err != nil {
		return lastSequence, fmt.Errorf("droids sqlite: insert initial record %s/%s: %w", record.Kind, record.ID, err)
	}
	return lastSequence, nil
}

func applyMutation(
	ctx context.Context,
	conn *sql.Conn,
	mutation droids.EncodedMutation,
	lastSequence uint64,
	revision uint64,
) (uint64, error) {
	var existingScope string
	var existingSequence sql.NullInt64
	err := conn.QueryRowContext(ctx, `
		SELECT scope, sequence FROM records WHERE record_kind = ? AND record_id = ?
	`, mutation.RecordKind, mutation.RecordID).Scan(&existingScope, &existingSequence)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return lastSequence, fmt.Errorf("droids sqlite: inspect record %s/%s: %w", mutation.RecordKind, mutation.RecordID, err)
	}

	if mutation.Operation == droids.MutationDelete {
		if !exists {
			return lastSequence, nil
		}
		if droids.RecordScope(existingScope) == droids.RecordHistory {
			return lastSequence, fmt.Errorf("droids sqlite: historical record %s/%s is immutable", mutation.RecordKind, mutation.RecordID)
		}
		if _, err := conn.ExecContext(ctx, `DELETE FROM records WHERE record_kind = ? AND record_id = ?`, mutation.RecordKind, mutation.RecordID); err != nil {
			return lastSequence, fmt.Errorf("droids sqlite: delete record %s/%s: %w", mutation.RecordKind, mutation.RecordID, err)
		}
		return lastSequence, nil
	}
	if err := validateRecord(mutation.RecordKind, mutation.RecordID, mutation.Scope, mutation.Version, mutation.Payload); err != nil {
		return lastSequence, err
	}
	if mutation.Operation == droids.MutationAssertAbsent && exists {
		return lastSequence, fmt.Errorf("droids sqlite: record %s/%s already exists", mutation.RecordKind, mutation.RecordID)
	}
	if mutation.Operation != droids.MutationPut && mutation.Operation != droids.MutationAssertAbsent {
		return lastSequence, fmt.Errorf("droids sqlite: unsupported mutation operation %q", mutation.Operation)
	}
	if exists && droids.RecordScope(existingScope) == droids.RecordHistory {
		return lastSequence, fmt.Errorf("droids sqlite: historical record %s/%s is immutable", mutation.RecordKind, mutation.RecordID)
	}

	var sequence any
	if mutation.Scope == droids.RecordHistory {
		lastSequence++
		sequence = lastSequence
	}
	if exists {
		_, err = conn.ExecContext(ctx, `
			UPDATE records
			SET scope = ?, sequence = ?, version = ?, payload = ?, updated_revision = ?
			WHERE record_kind = ? AND record_id = ?
		`, mutation.Scope, sequence, mutation.Version, []byte(mutation.Payload), revision, mutation.RecordKind, mutation.RecordID)
	} else {
		_, err = conn.ExecContext(ctx, `
			INSERT INTO records(
				record_kind, record_id, scope, sequence, version, payload,
				created_revision, updated_revision
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, mutation.RecordKind, mutation.RecordID, mutation.Scope, sequence, mutation.Version, []byte(mutation.Payload), revision, revision)
	}
	if err != nil {
		return lastSequence, fmt.Errorf("droids sqlite: write record %s/%s: %w", mutation.RecordKind, mutation.RecordID, err)
	}
	return lastSequence, nil
}

func insertEvent(
	ctx context.Context,
	conn *sql.Conn,
	event droids.EncodedDurableEvent,
	sequence droids.EventSequence,
	revision uint64,
) error {
	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO events(sequence, revision, kind, version, payload, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, sequence, revision, event.Kind, event.Version, []byte(event.Payload), sqliteTime(occurredAt)); err != nil {
		return fmt.Errorf("droids sqlite: insert event %d: %w", sequence, err)
	}
	return nil
}

func validateRecord(kind, id string, scope droids.RecordScope, version uint16, payload []byte) error {
	if kind == "" || id == "" {
		return fmt.Errorf("droids sqlite: record kind and id are required")
	}
	if scope != droids.RecordRuntime && scope != droids.RecordHistory {
		return fmt.Errorf("droids sqlite: unsupported record scope %q", scope)
	}
	if version == 0 || len(payload) == 0 {
		return fmt.Errorf("droids sqlite: record version and payload are required")
	}
	return nil
}

func validateEvent(event droids.EncodedDurableEvent) error {
	if event.Kind == "" || event.Version == 0 || len(event.Payload) == 0 {
		return fmt.Errorf("droids sqlite: event kind, version, and payload are required")
	}
	return nil
}

func pageSize(requested int) (int, error) {
	if requested < 0 || requested > maxPageSize {
		return 0, fmt.Errorf("droids sqlite: page limit must be between 0 and %d", maxPageSize)
	}
	if requested == 0 {
		return defaultPageSize, nil
	}
	return requested, nil
}

func sqliteTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY CHECK (version >= 1),
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("droids sqlite: create migration table: %w", err)
	}
	body, err := migrationFiles.ReadFile(initialMigration)
	if err != nil {
		return fmt.Errorf("droids sqlite: read initial migration: %w", err)
	}
	digest := sha256.Sum256(body)
	checksum := hex.EncodeToString(digest[:])
	var latest int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&latest); err != nil {
		return fmt.Errorf("droids sqlite: read migration version: %w", err)
	}
	if latest > 1 {
		return fmt.Errorf("droids sqlite: database schema version %d is newer than this build", latest)
	}
	var appliedChecksum string
	err = db.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version = 1`).Scan(&appliedChecksum)
	if err == nil {
		if appliedChecksum != checksum {
			return fmt.Errorf("droids sqlite: migration 1 checksum mismatch")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("droids sqlite: inspect migrations: %w", err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("droids sqlite: acquire migration connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("droids sqlite: begin migration: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if _, err := conn.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("droids sqlite: apply migration 1: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO schema_migrations(version, name, checksum, applied_at)
		VALUES (1, 'initial', ?, ?)
	`, checksum, sqliteTime(time.Now())); err != nil {
		return fmt.Errorf("droids sqlite: record migration 1: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("droids sqlite: commit migration 1: %w", err)
	}
	committed = true
	return nil
}
