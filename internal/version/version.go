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
	// Stable releases beginning at 0.39.0 that retain protocol 42 must remain
	// bidirectionally compatible; change the number for a baseline wire break.
	// RC and development builds are not covered by that release promise.
	//
	// 41: Subagent mailbox entries carry a kind instead of a task ID, allowing
	// parent-directed request replies alongside task-completion notifications.
	// 42: Session API errors use one code-bearing body for scratchpad operations
	// and requests rejected before routing.
	// 43: Turns replace runs throughout the session wire contract.
	// 44: The server chooses fork child IDs; fork requests no longer carry one.
	// Forks accept an optional first prompt and return ForkSessionResult, which
	// reports a first turn that could not start without failing the fork.
	// 45: Plugin-submitted messages: transcript plugin_message context boundaries
	// and the turn-scoped plugin.message.added live event.
	// 46: Prompt command invocations: the promptCommand transcript content kind
	// and the claude_project prompt command source (ADR 0042).
	SessionProtocolVersion = 46
)
