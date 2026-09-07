// Package version contains build and protocol version metadata.
package version

var (
	// Version is replaced by release builds with -ldflags.
	Version = "dev"
	// Commit is replaced by release builds with -ldflags.
	Commit = "unknown"
)

const (
	// LocalRegistryVersion versions the daemon discovery file.
	LocalRegistryVersion = 1
	// SessionProtocolVersion versions the server/session wire protocol.
	SessionProtocolVersion = 11
)
