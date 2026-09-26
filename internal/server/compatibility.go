package server

import (
	"errors"
	"fmt"

	"github.com/akonwi/kit/internal/version"
)

// ErrIncompatibleDaemon indicates an authenticated daemon that this client must not use.
var ErrIncompatibleDaemon = errors.New("local daemon is incompatible")

// ErrDaemonNotReady indicates an authenticated daemon whose database is not ready.
// Startup must not replace it or assume its database is safe for another process.
var ErrDaemonNotReady = errors.New("local daemon database is not ready")

// CompatibilityReason identifies why a verified daemon cannot serve this client.
type CompatibilityReason string

const (
	// DaemonProtocolOlder means the running daemon's session protocol is older.
	DaemonProtocolOlder CompatibilityReason = "daemon_protocol_older"
	// ClientProtocolOlder means the client's session protocol is older.
	ClientProtocolOlder CompatibilityReason = "client_protocol_older"
	// ReleaseMismatch means protocol versions match but release versions differ.
	ReleaseMismatch CompatibilityReason = "release_mismatch"
)

// DaemonCompatibilityError preserves the non-secret versions needed to explain a mismatch.
// It does not imply that replacing the running daemon is safe.
type DaemonCompatibilityError struct {
	Reason         CompatibilityReason
	ClientVersion  string
	DaemonVersion  string
	ClientProtocol int
	DaemonProtocol int
}

func (e *DaemonCompatibilityError) Error() string {
	if e == nil {
		return ErrIncompatibleDaemon.Error()
	}
	action := "Use a matching client; restart the daemon with the intended binary only when interrupting active work is acceptable."
	if e.Reason == ClientProtocolOlder {
		action = "Update this client; do not replace the newer daemon with an older binary."
	}
	return fmt.Sprintf("%s (%s): client %s (protocol %d), daemon %s (protocol %d); the daemon was left running. %s",
		ErrIncompatibleDaemon, e.Reason, e.ClientVersion, e.ClientProtocol, e.DaemonVersion, e.DaemonProtocol, action)
}

// IncompatibleDaemon marks this as a terminal client compatibility failure.
func (e *DaemonCompatibilityError) IncompatibleDaemon() bool { return true }

// Unwrap preserves errors.Is(err, ErrIncompatibleDaemon) for existing callers.
func (e *DaemonCompatibilityError) Unwrap() error { return ErrIncompatibleDaemon }

// CheckCompatibility compares the verified daemon registry with this client.
// Callers must authenticate and verify the registry against daemon health first.
func CheckCompatibility(registry Registry) error {
	return compatibleWithVersion(registry, version.Version)
}

func compatible(registry Registry) error { return CheckCompatibility(registry) }

func compatibleWithVersion(registry Registry, clientVersion string) error {
	base := DaemonCompatibilityError{
		ClientVersion: clientVersion, DaemonVersion: registry.KitVersion,
		ClientProtocol: version.SessionProtocolVersion, DaemonProtocol: registry.ProtocolVersion,
	}
	switch {
	case registry.ProtocolVersion < version.SessionProtocolVersion:
		base.Reason = DaemonProtocolOlder
	case registry.ProtocolVersion > version.SessionProtocolVersion:
		base.Reason = ClientProtocolOlder
	case clientVersion != "dev" && registry.KitVersion != clientVersion:
		base.Reason = ReleaseMismatch
	default:
		return nil
	}
	return &base
}
