package protocol

// ReservedCommandDomains returns Kit-owned top-level command names. External
// plugin IDs cannot claim these domains even before any commands register.
// Keep renderer command inventories covered by this server/client contract.
func ReservedCommandDomains() []string {
	return []string{"cd", "compact", "debug", "diff", "files", "fork", "login", "mcp", "model", "refresh-models", "name", "new", "quit", "reload", "scratchpad", "sessions", "subagents", "tabs", "theme", "thinking"}
}
