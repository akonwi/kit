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
	// Stable releases beginning at 0.37.0 that retain protocol 40 must remain
	// bidirectionally compatible; change the number for a baseline wire break.
	// RC and development builds are not covered by that release promise.
	//
	// 40: Transcript messages no longer carry role "bash" or a bash execution
	// payload. Direct shell work is read from the bash-history projection and
	// enters the transcript only as an included droid context boundary, so the
	// two sequence spaces can no longer disagree.
	SessionProtocolVersion = 40
)
