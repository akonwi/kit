package devices

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryRepository struct {
	mu      sync.Mutex
	records map[string]Record
	touches int
	// inserting, when set, receives each insert, which then waits for
	// release.
	inserting chan struct{}
	release   chan struct{}
}

func newMemoryRepository(records ...Record) *memoryRepository {
	repository := &memoryRepository{records: map[string]Record{}}
	for _, record := range records {
		repository.records[record.ID] = record
	}
	return repository
}

func (r *memoryRepository) InsertDevice(_ context.Context, record Record) error {
	if r.inserting != nil {
		r.inserting <- struct{}{}
		<-r.release
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[record.ID] = record
	return nil
}

func (r *memoryRepository) ListDevices(context.Context) ([]Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var records []Record
	for _, record := range r.records {
		records = append(records, record)
	}
	return records, nil
}

func (r *memoryRepository) DeleteDevice(_ context.Context, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.records[id]
	delete(r.records, id)
	return ok, nil
}

func (r *memoryRepository) TouchDevice(_ context.Context, id string, lastUsed time.Time, address string, expires time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[id]
	record.LastUsedAt, record.LastAddress, record.ExpiresAt = lastUsed, address, expires
	r.records[id] = record
	r.touches++
	return nil
}

func (r *memoryRepository) PurgeDevices(_ context.Context, cutoff time.Time) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []string
	for id, record := range r.records {
		if record.ExpiresAt.Before(cutoff) {
			ids = append(ids, id)
			delete(r.records, id)
		}
	}
	return ids, nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newTestService(t *testing.T, repository *memoryRepository) (*Service, *clock) {
	t.Helper()
	current := &clock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	service, err := NewService(t.Context(), repository, Options{Now: current.Now})
	if err != nil {
		t.Fatal(err)
	}
	return service, current
}

func TestIssuedTokensAuthenticateAndAreStoredHashed(t *testing.T) {
	repository := newMemoryRepository()
	service, current := newTestService(t, repository)
	record, token, err := service.Issue(t.Context(), "laptop", "100.64.0.2")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if !strings.HasPrefix(token, TokenPrefix) || !IsToken(token) || len(token) != len(TokenPrefix)+43 {
		t.Fatalf("token %q does not have the device token form", token)
	}
	if !strings.HasPrefix(record.ID, IDPrefix) || record.TokenHash != HashToken(token) {
		t.Fatalf("record = %+v", record)
	}
	if want := current.now.Add(IdleLifetime); !record.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", record.ExpiresAt, want)
	}
	if stored := repository.records[record.ID]; stored.TokenHash != HashToken(token) || strings.Contains(stored.Name, token) {
		t.Fatalf("stored record = %+v", stored)
	}

	device, err := service.Authenticate(t.Context(), token, "100.64.0.2")
	if err != nil || device.ID != record.ID || device.Revoked.Err() != nil {
		t.Fatalf("Authenticate() = %+v, %v", device, err)
	}
	for _, wrong := range []string{"", "kit_device_", token + "x", strings.Replace(token, TokenPrefix, "kit_other_", 1), TokenPrefix + strings.Repeat("A", 43)} {
		if _, err := service.Authenticate(t.Context(), wrong, ""); !errors.Is(err, ErrUnknown) {
			t.Errorf("Authenticate(%q) error = %v, want ErrUnknown", wrong, err)
		}
	}

	reloaded, err := NewService(t.Context(), repository, Options{Now: current.Now})
	if err != nil {
		t.Fatal(err)
	}
	if device, err := reloaded.Authenticate(t.Context(), token, ""); err != nil || device.ID != record.ID {
		t.Fatalf("Authenticate() after reload = %+v, %v", device, err)
	}
}

func TestUseIsRecordedAtMostOncePerMinuteAndExtendsExpiry(t *testing.T) {
	repository := newMemoryRepository()
	service, current := newTestService(t, repository)
	record, token, err := service.Issue(t.Context(), "laptop", "a")
	if err != nil {
		t.Fatal(err)
	}
	current.now = current.now.Add(30 * time.Second)
	if _, err := service.Authenticate(t.Context(), token, "b"); err != nil {
		t.Fatal(err)
	}
	if repository.touches != 0 {
		t.Fatalf("touches within a minute = %d, want 0", repository.touches)
	}
	current.now = current.now.Add(time.Minute)
	if _, err := service.Authenticate(t.Context(), token, "c"); err != nil {
		t.Fatal(err)
	}
	stored := repository.records[record.ID]
	if repository.touches != 1 || !stored.LastUsedAt.Equal(current.now) || stored.LastAddress != "c" || !stored.ExpiresAt.Equal(current.now.Add(IdleLifetime)) {
		t.Fatalf("touches = %d, stored = %+v", repository.touches, stored)
	}
	if listed := service.List(); len(listed) != 1 || listed[0].LastAddress != "c" {
		t.Fatalf("List() = %+v", listed)
	}
}

func TestTokensExpireWhenIdleAndAtTheAbsoluteLimit(t *testing.T) {
	service, current := newTestService(t, newMemoryRepository())
	_, idle, _ := service.Issue(t.Context(), "idle", "")
	_, busy, _ := service.Issue(t.Context(), "busy", "")
	if service.Active() != 2 {
		t.Fatalf("Active() = %d, want 2", service.Active())
	}
	for elapsed := time.Duration(0); elapsed < MaxLifetime; elapsed += IdleLifetime / 2 {
		current.now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(elapsed)
		if _, err := service.Authenticate(t.Context(), busy, ""); err != nil {
			t.Fatalf("busy device at %v: %v", elapsed, err)
		}
	}
	if _, err := service.Authenticate(t.Context(), idle, ""); !errors.Is(err, ErrExpired) {
		t.Fatalf("idle device error = %v, want ErrExpired", err)
	}
	current.now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(MaxLifetime)
	if _, err := service.Authenticate(t.Context(), busy, ""); !errors.Is(err, ErrExpired) {
		t.Fatalf("busy device after a year error = %v, want ErrExpired", err)
	}
	if service.Active() != 0 {
		t.Fatalf("Active() = %d, want 0", service.Active())
	}
}

func TestPurgeKeepsRecentlyExpiredDevices(t *testing.T) {
	service, current := newTestService(t, newMemoryRepository())
	_, token, _ := service.Issue(t.Context(), "laptop", "")
	current.now = current.now.Add(IdleLifetime + time.Hour)
	if err := service.Purge(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(t.Context(), token, ""); !errors.Is(err, ErrExpired) {
		t.Fatalf("recently expired error = %v, want ErrExpired", err)
	}
	current.now = current.now.Add(purgeGrace)
	if err := service.Purge(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(t.Context(), token, ""); !errors.Is(err, ErrUnknown) {
		t.Fatalf("purged error = %v, want ErrUnknown", err)
	}
}

func TestRevocationEndsTheDevicesRequests(t *testing.T) {
	repository := newMemoryRepository()
	service, _ := newTestService(t, repository)
	record, token, _ := service.Issue(t.Context(), "laptop", "")
	device, err := service.Authenticate(t.Context(), token, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Revoke(t.Context(), record.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	select {
	case <-device.Revoked.Done():
	default:
		t.Fatal("revocation did not end the device's requests")
	}
	if _, ok := repository.records[record.ID]; ok {
		t.Fatal("revoked device is still stored")
	}
	if _, err := service.Authenticate(t.Context(), token, ""); !errors.Is(err, ErrUnknown) {
		t.Fatalf("revoked token error = %v, want ErrUnknown", err)
	}
	if err := service.Revoke(t.Context(), record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Revoke() error = %v, want ErrNotFound", err)
	}
}

func TestIssueValidatesNames(t *testing.T) {
	service, _ := newTestService(t, newMemoryRepository())
	for _, name := range []string{"", " laptop", "laptop ", strings.Repeat("a", MaxNameBytes+1)} {
		if _, _, err := service.Issue(t.Context(), name, ""); err == nil {
			t.Errorf("Issue(%q) succeeded", name)
		}
	}
}

func TestIssueStopsAtCapacity(t *testing.T) {
	repository := newMemoryRepository()
	for index := range MaxDevices {
		id := fmt.Sprintf("%s%032x", IDPrefix, index)
		repository.records[id] = Record{ID: id, Name: "d", TokenHash: HashToken(id), ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	}
	service, _ := newTestService(t, repository)
	if _, _, err := service.Issue(t.Context(), "one more", ""); !errors.Is(err, ErrCapacity) {
		t.Fatalf("Issue() at capacity error = %v, want ErrCapacity", err)
	}
}

func TestConcurrentIssuesCannotExceedCapacity(t *testing.T) {
	repository := newMemoryRepository()
	for index := range MaxDevices - 1 {
		id := fmt.Sprintf("%s%032x", IDPrefix, index)
		repository.records[id] = Record{ID: id, Name: "d", TokenHash: HashToken(id), ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	}
	service, _ := newTestService(t, repository)
	repository.inserting, repository.release = make(chan struct{}), make(chan struct{})
	issued := make(chan error, 1)
	go func() {
		_, _, err := service.Issue(t.Context(), "last", "")
		issued <- err
	}()
	// The last place is held while its device is stored.
	<-repository.inserting
	if _, _, err := service.Issue(t.Context(), "one more", ""); !errors.Is(err, ErrCapacity) {
		t.Fatalf("Issue() while the last place is held: error = %v, want ErrCapacity", err)
	}
	close(repository.release)
	if err := <-issued; err != nil {
		t.Fatalf("Issue() of the last place: %v", err)
	}
}

func TestNamesCannotShadowDeviceIDs(t *testing.T) {
	if err := ValidateName(IDPrefix + strings.Repeat("0", 32)); err == nil {
		t.Fatal("a device ID was accepted as a name")
	}
	if err := ValidateName("device_laptop"); err != nil {
		t.Fatalf("ValidateName(device_laptop) = %v", err)
	}
}

func TestAuthenticatedDevicesCarryTheirExpiry(t *testing.T) {
	service, current := newTestService(t, newMemoryRepository())
	_, token, _ := service.Issue(t.Context(), "laptop", "")
	current.now = current.now.Add(2 * time.Minute)
	device, err := service.Authenticate(t.Context(), token, "")
	if err != nil || !device.Expires.Equal(current.now.Add(IdleLifetime)) {
		t.Fatalf("Authenticate() = %+v, %v", device, err)
	}
}
