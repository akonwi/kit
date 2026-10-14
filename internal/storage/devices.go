package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/akonwi/kit/internal/devices"
)

var _ devices.Repository = (*Store)(nil)

// InsertDevice stores a newly issued device.
func (s *Store) InsertDevice(ctx context.Context, record devices.Record) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO devices (id, name, token_hash, created_at, last_used_at, last_address, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, record.ID, record.Name, record.TokenHash[:], formatTimestamp(record.CreatedAt),
		formatTimestamp(record.LastUsedAt), record.LastAddress, formatTimestamp(record.ExpiresAt))
	if err != nil {
		return fmt.Errorf("insert device: %w", err)
	}
	return nil
}

// ListDevices returns every stored device.
func (s *Store) ListDevices(ctx context.Context) ([]devices.Record, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, token_hash, created_at, last_used_at, last_address, expires_at
		FROM devices ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	var records []devices.Record
	for rows.Next() {
		var record devices.Record
		var hash []byte
		var created, lastUsed, expires string
		if err := rows.Scan(&record.ID, &record.Name, &hash, &created, &lastUsed, &record.LastAddress, &expires); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		if len(hash) != len(record.TokenHash) {
			return nil, fmt.Errorf("device %s has a malformed token hash", record.ID)
		}
		copy(record.TokenHash[:], hash)
		for _, field := range []struct {
			value  string
			target *time.Time
		}{{created, &record.CreatedAt}, {lastUsed, &record.LastUsedAt}, {expires, &record.ExpiresAt}} {
			parsed, err := parseTimestamp(field.value)
			if err != nil {
				return nil, fmt.Errorf("device %s timestamp: %w", record.ID, err)
			}
			*field.target = parsed
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	return records, nil
}

// DeleteDevice deletes one device and reports whether it existed.
func (s *Store) DeleteDevice(ctx context.Context, id string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("store is closed")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete device: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete device: %w", err)
	}
	return count > 0, nil
}

// TouchDevice records a device's latest use and the expiry it extends to.
func (s *Store) TouchDevice(ctx context.Context, id string, lastUsed time.Time, address string, expires time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE devices SET last_used_at = ?, last_address = ?, expires_at = ? WHERE id = ?
	`, formatTimestamp(lastUsed), address, formatTimestamp(expires), id)
	if err != nil {
		return fmt.Errorf("touch device: %w", err)
	}
	return nil
}

// PurgeDevices deletes devices that expired before cutoff and returns their
// IDs. Timestamps share one fixed-width UTC layout, so they compare as text.
func (s *Store) PurgeDevices(ctx context.Context, cutoff time.Time) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is closed")
	}
	rows, err := s.db.QueryContext(ctx, `DELETE FROM devices WHERE expires_at < ? RETURNING id`, formatTimestamp(cutoff))
	if err != nil {
		return nil, fmt.Errorf("purge devices: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("purge devices: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("purge devices: %w", err)
	}
	return ids, nil
}
