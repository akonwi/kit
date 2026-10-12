package localdiscovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/securefs"
)

// Publish makes a daemon discoverable. The caller must hold the daemon
// lifetime lock. The token is written before the registry, which is the
// publication marker, so clients never discover a registry without its token.
// Neither file may already exist; on failure nothing remains published.
func Publish(paths apphome.Paths, token string, registry Registry) error {
	if err := validateToken(token); err != nil {
		return err
	}
	if err := registry.validate(); err != nil {
		return fmt.Errorf("validate daemon registry: %w", err)
	}
	body, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode daemon registry: %w", err)
	}
	body = append(body, '\n')
	if err := writePrivateFile(paths.ServerToken, []byte(token+"\n")); err != nil {
		return fmt.Errorf("publish daemon token: %w", err)
	}
	if err := writePrivateFile(paths.ServerRegistry, body); err != nil {
		return errors.Join(fmt.Errorf("publish daemon registry: %w", err), removeIfExists(paths.ServerToken))
	}
	return nil
}

// Clear removes a publication left by a process that no longer runs. Callers
// must hold the daemon lifetime lock, which proves no live daemon owns it.
func Clear(paths apphome.Paths) error {
	for _, path := range []string{paths.ServerRegistry, paths.ServerToken} {
		if err := removeIfExists(path); err != nil {
			return fmt.Errorf("remove stale daemon registration: %w", err)
		}
	}
	return nil
}

// Withdraw removes instanceID's publication during daemon shutdown. A
// publication that belongs to another instance is left untouched. The registry
// is removed first so clients cannot discover a token while it is deleted.
// Withdraw removes both files even when it reports an unreadable registry.
func Withdraw(paths apphome.Paths, instanceID string) error {
	registry, err := Load(paths)
	if err == nil && registry.InstanceID != instanceID {
		return nil
	}
	var failures error
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		failures = fmt.Errorf("daemon registry became unreadable during withdrawal: %w", err)
	}
	for _, path := range []string{paths.ServerRegistry, paths.ServerToken} {
		failures = errors.Join(failures, removeIfExists(path))
	}
	return failures
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %q: %w", path, err)
	}
	return nil
}

func writePrivateFile(path string, body []byte) error {
	if err := securefs.MakePrivateDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("create private file directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".kit-write-*")
	if err != nil {
		return fmt.Errorf("create temporary private file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := securefs.ProtectFile(temporaryPath); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary private file: %w", err)
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary private file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary private file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary private file: %w", err)
	}
	if err := publishNoReplace(temporaryPath, path); err != nil {
		return fmt.Errorf("publish private file %q: %w", path, err)
	}
	return nil
}
