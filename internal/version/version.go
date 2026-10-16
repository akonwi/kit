// Package version contains build and protocol version metadata.
package version

var (
	// Version is replaced by release builds with -ldflags.
	Version = "dev"
	// Commit is replaced by release builds with -ldflags.
	Commit = "unknown"
	// Channel names the isolated Kit home this build binds to when KIT_HOME is
	// unset. Empty means the usual shared home, which is what a release build
	// wants; naming a channel is what opts a build into isolated state.
	Channel = ""
)

const (
	// LocalRegistryVersion versions the daemon discovery file.
	LocalRegistryVersion = 1
	// SessionProtocolVersion versions the server/session wire protocol.
	// Clients and servers are compatible exactly when this number matches,
	// regardless of release label; change it for any wire break.
	//
	// 41: Subagent mailbox entries carry a kind instead of a task ID, allowing
	// parent-directed request replies alongside task-completion notifications.
	// 42: Session API errors use one code-bearing body for scratchpad operations
	// and requests rejected before routing.
	// 43: Turns replace runs throughout the session wire contract.
	// 44: The server chooses fork child IDs; fork requests no longer carry one.
	// Forks accept an optional first prompt and return ForkSessionResult, which
	// reports a first turn that could not start without failing the fork.
	// 45: Plugin-submitted messages add transcript plugin_message context
	// boundaries and the turn-scoped plugin.message.added live event. Prompt
	// command invocations add the promptCommand transcript content kind and the
	// claude_project prompt command source (ADRs 0041 and 0042).
	// 46: Health reports the server's stable identity, Kit-scoped machine
	// identifier, and user home directory (ADR 0045).
	// 47: Requests authenticate with device tokens as well as the local token,
	// so every operation's error union adds credential_expired (401) and
	// rate_limited (429). Adds pairing, device, and provider login operations
	// (ADR 0045).
	SessionProtocolVersion = 47
)
