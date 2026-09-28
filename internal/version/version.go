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
	// Stable releases beginning at 0.38.0 that retain protocol 41 must remain
	// bidirectionally compatible; change the number for a baseline wire break.
	// RC and development builds are not covered by that release promise.
	//
	// 41: Subagent mailbox entries carry a kind instead of a task ID, allowing
	// parent-directed request replies alongside task-completion notifications.
	SessionProtocolVersion = 41
)
