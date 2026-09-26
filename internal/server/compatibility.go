package server

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

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
	// ReleaseMismatch means equal protocols but release compatibility is unverified.
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
// Stable releases starting at 0.37.0 share the protocol-40 compatibility promise;
// development and prerelease builds do not establish that promise.
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
	case registry.KitVersion == clientVersion:
		// Preserve exact-label local development attachment. This does not
		// assert compatibility between independently built dev worktrees.
		return nil
	case coveredSessionReleasePair(version.SessionProtocolVersion, clientVersion, registry.KitVersion):
		return nil
	default:
		base.Reason = ReleaseMismatch
	}
	return &base
}

// coveredSessionReleasePair limits cross-release attachment to the protocol
// whose baseline contract begins with stable Kit 0.37.0. A future protocol
// number must explicitly establish its own release eligibility.
func coveredSessionReleasePair(protocol int, clientVersion, daemonVersion string) bool {
	return protocol == 40 && protocol40Release(clientVersion) && protocol40Release(daemonVersion)
}

// protocol40Release recognizes canonical stable releases covered by the first
// protocol-40 release. RC and development labels remain outside the promise.
func protocol40Release(release string) bool {
	if len(release) == 0 || len(release) > 64 {
		return false
	}
	parts := strings.Split(release, ".")
	if len(parts) != 3 {
		return false
	}
	var numbers [3]uint64
	for index, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return false
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return false
			}
		}
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return false
		}
		numbers[index] = number
	}
	return numbers[0] > 0 || numbers[0] == 0 && numbers[1] >= 37
}
