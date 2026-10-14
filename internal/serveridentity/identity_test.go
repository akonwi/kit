package serveridentity

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

func TestServerIDIsCreatedOnceAndPersisted(t *testing.T) {
	t.Parallel()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))

	first, err := ServerID(paths)
	if err != nil {
		t.Fatalf("ServerID() error = %v", err)
	}
	if !ValidServerID(first) {
		t.Fatalf("ServerID() = %q, want a server_ identifier", first)
	}
	second, err := ServerID(paths)
	if err != nil {
		t.Fatalf("second ServerID() error = %v", err)
	}
	if second != first {
		t.Fatalf("second ServerID() = %q, want persisted %q", second, first)
	}
	info, err := os.Stat(paths.ServerID)
	if err != nil {
		t.Fatalf("stat server id: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("server id mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestConcurrentServerIDCreationConverges(t *testing.T) {
	t.Parallel()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))

	const creators = 16
	results := make([]string, creators)
	failures := make([]error, creators)
	var wait sync.WaitGroup
	for index := range creators {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results[index], failures[index] = ServerID(paths)
		}()
	}
	wait.Wait()
	for index := range creators {
		if failures[index] != nil {
			t.Fatalf("creator %d error = %v", index, failures[index])
		}
		if results[index] != results[0] {
			t.Fatalf("creator %d = %q, want %q", index, results[index], results[0])
		}
	}
}

func TestMalformedServerIDIsNotReplaced(t *testing.T) {
	t.Parallel()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ServerID, []byte("not-an-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ServerID(paths); err == nil {
		t.Fatal("ServerID() accepted a malformed identity file")
	}
	body, err := os.ReadFile(paths.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "not-an-id\n" {
		t.Fatalf("identity file = %q, want it left unchanged", body)
	}
}

// Tests that replace platformID run sequentially.

func TestMachineIDHashesThePlatformIdentifier(t *testing.T) {
	restore := replacePlatformID(func() (string, error) { return "4C1E7F7A-0000-4000-8000-1234567890AB", nil })
	defer restore()
	first := apphome.FromHome(filepath.Join(t.TempDir(), "first"))
	second := apphome.FromHome(filepath.Join(t.TempDir(), "second"))

	firstID, err := MachineID(first)
	if err != nil {
		t.Fatalf("MachineID(first) error = %v", err)
	}
	secondID, err := MachineID(second)
	if err != nil {
		t.Fatalf("MachineID(second) error = %v", err)
	}
	want := hashMachineID("4C1E7F7A-0000-4000-8000-1234567890AB")
	if firstID != want || secondID != want {
		t.Fatalf("MachineID() = %q and %q, want %q for every home on the machine", firstID, secondID, want)
	}
	if !ValidMachineID(firstID) {
		t.Fatalf("MachineID() = %q, want 64 lowercase hex characters", firstID)
	}
	if _, err := os.Stat(first.MachineID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("platform machine id wrote a fallback file: %v", err)
	}
}

func TestMachineIDFallsBackToAHomeScopedValue(t *testing.T) {
	restore := replacePlatformID(func() (string, error) { return "", errors.New("unavailable") })
	defer restore()
	home := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	other := apphome.FromHome(filepath.Join(t.TempDir(), "other"))

	first, err := MachineID(home)
	if err != nil {
		t.Fatalf("MachineID() error = %v", err)
	}
	again, err := MachineID(home)
	if err != nil {
		t.Fatalf("second MachineID() error = %v", err)
	}
	elsewhere, err := MachineID(other)
	if err != nil {
		t.Fatalf("MachineID(other) error = %v", err)
	}
	if first != again {
		t.Fatalf("fallback MachineID() = %q then %q, want a stable value", first, again)
	}
	if first == elsewhere {
		t.Fatalf("fallback MachineID() = %q for two homes, want home-scoped values", first)
	}
	if !ValidMachineID(first) {
		t.Fatalf("fallback MachineID() = %q, want 64 lowercase hex characters", first)
	}
}

func replacePlatformID(read func() (string, error)) func() {
	previous := platformID
	platformID = read
	return func() { platformID = previous }
}
