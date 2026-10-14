// Package serveridentity owns the stable identity a Kit server reports to its
// clients: a server identifier persisted in the Kit home, and a Kit-scoped
// machine identifier that lets clients decide whether a server runs on their
// own machine.
package serveridentity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/securefs"
)

// ServerIDPrefix prefixes every server identifier.
const ServerIDPrefix = "server_"

// machineIDDomain separates Kit machine identifiers from other uses of the
// platform identifier, so the reported value cannot be correlated with it.
const machineIDDomain = "kit-machine-id-v1\x00"

// ValidServerID reports whether value is a well-formed server identifier.
func ValidServerID(value string) bool {
	return identifier.Valid(value, ServerIDPrefix)
}

// ValidMachineID reports whether value is a well-formed machine identifier.
func ValidMachineID(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// ServerID returns the server identifier of the Kit home, creating and
// persisting it on first use. The identifier is stable for the lifetime of the
// home and independent of the running server process.
func ServerID(paths apphome.Paths) (string, error) {
	return loadOrCreate(paths.ServerID, func() (string, error) {
		return identifier.New(ServerIDPrefix)
	}, ValidServerID)
}

// MachineID returns the Kit-scoped identifier of the current machine. It is
// derived from the platform machine identifier (IOPlatformUUID on macOS,
// /etc/machine-id on Linux). Where the platform identifier is unavailable, as
// in many containers, it is derived from a random value persisted in the Kit
// home, so only processes sharing that home recognize each other as
// same-machine.
func MachineID(paths apphome.Paths) (string, error) {
	if raw, err := platformID(); err == nil {
		return hashMachineID(raw), nil
	}
	raw, err := loadOrCreate(paths.MachineID, randomHex, validRandomHex)
	if err != nil {
		return "", err
	}
	return hashMachineID("home:" + raw), nil
}

// platformID reads the platform machine identifier. Tests replace it.
var platformID = cachedPlatformMachineID

var platformMachineID = struct {
	once  sync.Once
	value string
	err   error
}{}

func cachedPlatformMachineID() (string, error) {
	platformMachineID.once.Do(func() {
		platformMachineID.value, platformMachineID.err = readPlatformMachineID()
	})
	return platformMachineID.value, platformMachineID.err
}

func hashMachineID(raw string) string {
	sum := sha256.Sum256([]byte(machineIDDomain + raw))
	return hex.EncodeToString(sum[:])
}

func randomHex() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate machine identifier: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func validRandomHex(value string) bool {
	return ValidMachineID(value)
}

// loadOrCreate reads an identifier file, or atomically publishes a newly
// generated value when none exists. Concurrent creators converge on the first
// published value. An existing malformed file is an error rather than being
// replaced, because replacing it would silently change the identity.
func loadOrCreate(path string, generate func() (string, error), valid func(string) bool) (string, error) {
	if path == "" {
		return "", errors.New("identity path is empty")
	}
	value, err := read(path, valid)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return value, err
	}
	generated, err := generate()
	if err != nil {
		return "", err
	}
	if err := publish(path, generated); err != nil {
		if errors.Is(err, os.ErrExist) {
			return read(path, valid)
		}
		return "", err
	}
	return generated, nil
}

func read(path string, valid func(string) bool) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(body))
	if !valid(value) {
		return "", fmt.Errorf("identity file %q is malformed", path)
	}
	return value, nil
}

func publish(path, value string) error {
	directory := filepath.Dir(path)
	if err := securefs.MakePrivateDir(directory); err != nil {
		return fmt.Errorf("create identity directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".kit-identity-*")
	if err != nil {
		return fmt.Errorf("create temporary identity file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := securefs.ProtectFile(temporaryPath); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary identity file: %w", err)
	}
	if _, err := temporary.WriteString(value + "\n"); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary identity file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary identity file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary identity file: %w", err)
	}
	// Linking fails when the destination exists, so a concurrent creator's
	// complete file is never replaced or observed partially written.
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	return nil
}
