// Package devices owns device tokens: the durable credentials of clients that
// address a Kit server explicitly. Tokens are issued only by redeeming a
// pairing code, stored as SHA-256 hashes, expire after idle and absolute
// lifetimes, and can be revoked, which ends the device's open requests.
package devices

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/identifier"
)

const (
	// TokenPrefix marks device tokens for secret scanning.
	TokenPrefix = "kit_device_"
	// IDPrefix begins every device ID.
	IDPrefix = "device_"
	// IdleLifetime is how long a device token stays valid without use.
	IdleLifetime = 30 * 24 * time.Hour
	// MaxLifetime is how long a device token stays valid after it is issued.
	MaxLifetime = 365 * 24 * time.Hour
	// MaxNameBytes bounds a device name.
	MaxNameBytes = 64
	// MaxDevices bounds how many devices a server keeps, including expired
	// devices not yet purged.
	MaxDevices = 1024

	// touchInterval throttles last-use updates per device.
	touchInterval = time.Minute
	// purgeGrace keeps expired devices long enough for their clients to learn
	// that the credential expired rather than that it is unknown.
	purgeGrace = 30 * 24 * time.Hour
	tokenBytes = 32
)

var (
	// ErrUnknown reports a token that names no device.
	ErrUnknown = errors.New("device token is unknown")
	// ErrExpired reports a token whose device expired.
	ErrExpired = errors.New("device token expired")
	// ErrNotFound reports a device ID that names no device.
	ErrNotFound = errors.New("device not found")
	// ErrCapacity reports a server that already has MaxDevices devices.
	ErrCapacity = errors.New("too many paired devices; revoke one first")
)

// Hash is the stored form of a device token.
type Hash [sha256.Size]byte

// HashToken returns the stored form of token.
func HashToken(token string) Hash { return sha256.Sum256([]byte(token)) }

// IsToken reports whether value has the device token form.
func IsToken(value string) bool {
	if !strings.HasPrefix(value, TokenPrefix) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value[len(TokenPrefix):])
	return err == nil && len(decoded) == tokenBytes
}

// Record is one stored device.
type Record struct {
	ID          string
	Name        string
	TokenHash   Hash
	CreatedAt   time.Time
	LastUsedAt  time.Time
	LastAddress string
	ExpiresAt   time.Time
}

// expiry is the earlier of the idle and absolute lifetimes.
func expiry(created, lastUsed time.Time) time.Time {
	idle := lastUsed.Add(IdleLifetime)
	if absolute := created.Add(MaxLifetime); absolute.Before(idle) {
		return absolute
	}
	return idle
}

// Repository persists devices.
type Repository interface {
	InsertDevice(context.Context, Record) error
	ListDevices(context.Context) ([]Record, error)
	// DeleteDevice reports whether a device was deleted.
	DeleteDevice(context.Context, string) (bool, error)
	TouchDevice(ctx context.Context, id string, lastUsed time.Time, address string, expires time.Time) error
	// PurgeDevices deletes devices that expired before the time and returns
	// their IDs.
	PurgeDevices(context.Context, time.Time) ([]string, error)
}

// Device is an authenticated device.
type Device struct {
	ID string
	// Revoked is done once the device is revoked or purged.
	Revoked context.Context
	// Expires is when the device's token expires unless it is used again.
	Expires time.Time
}

type entry struct {
	record  Record
	revoked context.Context
	revoke  context.CancelFunc
}

// Service authenticates and manages devices. It caches every device in memory;
// the repository is changed only through the service.
type Service struct {
	repository Repository
	now        func() time.Time
	logger     *slog.Logger

	mu     sync.Mutex
	byHash map[Hash]*entry
	byID   map[string]*entry
}

// Options configures a Service.
type Options struct {
	// Now returns the current time; time.Now by default.
	Now func() time.Time
	// Logger receives failed last-use updates; nil discards them.
	Logger *slog.Logger
}

// NewService loads the stored devices.
func NewService(ctx context.Context, repository Repository, options Options) (*Service, error) {
	if repository == nil {
		return nil, errors.New("device repository is nil")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	records, err := repository.ListDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("load devices: %w", err)
	}
	service := &Service{
		repository: repository, now: options.Now, logger: options.Logger,
		byHash: make(map[Hash]*entry, len(records)), byID: make(map[string]*entry, len(records)),
	}
	for _, record := range records {
		service.add(record)
	}
	return service, nil
}

func (s *Service) add(record Record) {
	revoked, revoke := context.WithCancel(context.Background())
	stored := &entry{record: record, revoked: revoked, revoke: revoke}
	s.byHash[record.TokenHash] = stored
	s.byID[record.ID] = stored
}

func (s *Service) remove(id string) bool {
	stored, ok := s.byID[id]
	if !ok {
		return false
	}
	delete(s.byID, id)
	delete(s.byHash, stored.record.TokenHash)
	stored.revoke()
	return true
}

// ValidateName reports whether name can label a device: non-empty without
// surrounding space, at most MaxNameBytes bytes, and not shaped like a device
// ID, so a name never shadows an ID.
func ValidateName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || len(name) > MaxNameBytes {
		return fmt.Errorf("device name must be non-empty, trimmed, and at most %d bytes", MaxNameBytes)
	}
	if identifier.Valid(name, IDPrefix) {
		return errors.New("device name must not be a device ID")
	}
	return nil
}

// Issue creates a device named name and returns it with its token, which is
// never stored.
func (s *Service) Issue(ctx context.Context, name, address string) (Record, string, error) {
	if err := ValidateName(name); err != nil {
		return Record{}, "", err
	}
	s.mu.Lock()
	full := len(s.byID) >= MaxDevices
	s.mu.Unlock()
	if full {
		return Record{}, "", ErrCapacity
	}
	id, err := identifier.New(IDPrefix)
	if err != nil {
		return Record{}, "", err
	}
	secret := make([]byte, tokenBytes)
	if _, err := rand.Read(secret); err != nil {
		return Record{}, "", fmt.Errorf("generate device token: %w", err)
	}
	token := TokenPrefix + base64.RawURLEncoding.EncodeToString(secret)
	now := s.now().UTC()
	record := Record{
		ID: id, Name: name, TokenHash: HashToken(token),
		CreatedAt: now, LastUsedAt: now, LastAddress: address, ExpiresAt: expiry(now, now),
	}
	if err := s.repository.InsertDevice(ctx, record); err != nil {
		return Record{}, "", fmt.Errorf("store device: %w", err)
	}
	s.mu.Lock()
	s.add(record)
	s.mu.Unlock()
	return record, token, nil
}

// Authenticate returns the device whose token is token, recording its use from
// address at most once per minute. It fails with ErrUnknown or ErrExpired.
func (s *Service) Authenticate(ctx context.Context, token, address string) (Device, error) {
	if !IsToken(token) {
		return Device{}, ErrUnknown
	}
	now := s.now().UTC()
	s.mu.Lock()
	stored, ok := s.byHash[HashToken(token)]
	if !ok {
		s.mu.Unlock()
		return Device{}, ErrUnknown
	}
	if !now.Before(stored.record.ExpiresAt) {
		s.mu.Unlock()
		return Device{}, ErrExpired
	}
	device := Device{ID: stored.record.ID, Revoked: stored.revoked}
	touch := now.Sub(stored.record.LastUsedAt) >= touchInterval
	if touch {
		stored.record.LastUsedAt = now
		stored.record.LastAddress = address
		stored.record.ExpiresAt = expiry(stored.record.CreatedAt, now)
	}
	touched := stored.record
	device.Expires = touched.ExpiresAt
	s.mu.Unlock()
	if touch {
		// The use is already recorded in memory; a client that disconnects
		// must not cancel its write.
		writeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := s.repository.TouchDevice(writeContext, touched.ID, touched.LastUsedAt, touched.LastAddress, touched.ExpiresAt)
		cancel()
		if err != nil {
			s.logger.Warn("record device use", "device", touched.ID, "error", err)
		}
	}
	return device, nil
}

// List returns every device, including expired ones not yet purged, most
// recently used first.
func (s *Service) List() []Record {
	s.mu.Lock()
	records := make([]Record, 0, len(s.byID))
	for _, stored := range s.byID {
		records = append(records, stored.record)
	}
	s.mu.Unlock()
	slices.SortFunc(records, func(a, b Record) int {
		if order := b.LastUsedAt.Compare(a.LastUsedAt); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return records
}

// Active reports how many devices have not expired.
func (s *Service) Active() int {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, stored := range s.byID {
		if now.Before(stored.record.ExpiresAt) {
			count++
		}
	}
	return count
}

// Revoke deletes the device with id and ends its open requests.
func (s *Service) Revoke(ctx context.Context, id string) error {
	deleted, err := s.repository.DeleteDevice(ctx, id)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	s.mu.Lock()
	removed := s.remove(id)
	s.mu.Unlock()
	if !deleted && !removed {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return nil
}

// Purge deletes devices that expired more than 30 days ago. Until then an
// expired token is reported as expired rather than unknown.
func (s *Service) Purge(ctx context.Context) error {
	purged, err := s.repository.PurgeDevices(ctx, s.now().UTC().Add(-purgeGrace))
	if err != nil {
		return fmt.Errorf("purge expired devices: %w", err)
	}
	s.mu.Lock()
	for _, id := range purged {
		s.remove(id)
	}
	s.mu.Unlock()
	return nil
}

// RunPurge purges expired devices every interval until ctx ends.
func (s *Service) RunPurge(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Purge(ctx); err != nil && ctx.Err() == nil {
			s.logger.Warn("purge expired devices", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
