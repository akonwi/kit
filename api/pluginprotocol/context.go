package pluginprotocol

import "github.com/akonwi/kit/api/contract"

// InitializeParams opens a conversation. It is Kit's first message.
type InitializeParams struct {
	ProtocolVersion int          `json:"protocolVersion" schema:"const=2"`
	Kit             KitInfo      `json:"kit"`
	Instance        InstanceInfo `json:"instance"`
	Context         Context      `json:"context"`
}

// KitInfo describes the Kit server speaking to the plugin.
type KitInfo struct {
	Version string `json:"version" schema:"minLength=1" doc:"Kit server version, for diagnostics."`
}

// InstanceInfo identifies one generation of the plugin in its session.
type InstanceInfo struct {
	ID string `json:"id" schema:"minLength=1" doc:"Opaque identifier of this generation. Clients see the same identifier on the plugin's toasts, dialogs, footer items, and commands. A replacement generation has a new one."`
}

// InitializeResult accepts the conversation.
type InitializeResult struct {
	ProtocolVersion int `json:"protocolVersion" schema:"const=2"`
}

// Context is the session and project the plugin instance belongs to.
type Context struct {
	Project Project `json:"project"`
	Session Session `json:"session"`
}

// Project is the session's working directory and its Git state.
type Project struct {
	Cwd string `json:"cwd" schema:"minLength=1" doc:"Absolute working directory."`
	Git *Git   `json:"git" schema:"nullable" doc:"Null outside a Git worktree or when Git cannot be inspected safely."`
}

// Git is the working directory's Git state.
type Git struct {
	Root        string       `json:"root" schema:"minLength=1" doc:"Absolute worktree root."`
	Branch      *string      `json:"branch" schema:"nullable" doc:"Null for a detached head."`
	Dirty       bool         `json:"dirty"`
	PullRequest *PullRequest `json:"pullRequest" schema:"nullable" doc:"The branch's pull request; null when there is none or it is not known yet."`
}

// PullRequest is the GitHub pull request of the current branch.
type PullRequest struct {
	Number int                         `json:"number" schema:"minimum=1,maximum=9007199254740991"`
	URL    string                      `json:"url" schema:"minLength=1,maxLength=4096"`
	Checks *contract.PullRequestChecks `json:"checks,omitempty" doc:"Summary of the pull request's checks."`
}

// Session identifies the session the plugin instance belongs to.
type Session struct {
	ID   string  `json:"id" schema:"minLength=1"`
	Name *string `json:"name" schema:"nullable"`
}

// GitChangedParams carries the working directory's new Git state.
type GitChangedParams struct {
	Git *Git `json:"git" schema:"nullable"`
}

// TurnStartedParams reports a turn Kit admitted, before model or tool work.
type TurnStartedParams struct {
	SessionID string `json:"sessionId" schema:"minLength=1"`
	TurnID    string `json:"turnId" schema:"minLength=1"`
}

// TurnCompletedParams reports a settled turn's text.
type TurnCompletedParams struct {
	SessionID string `json:"sessionId" schema:"minLength=1"`
	Turn      Turn   `json:"turn"`
}

// Turn is the text-only projection of a completed turn.
type Turn struct {
	ID       string    `json:"id" schema:"minLength=1"`
	Messages []Message `json:"messages"`
}

// Message is one user or assistant message's text.
type Message struct {
	Role    MessageRole   `json:"role"`
	Content []TextContent `json:"content"`
}

// MessageRole is a turn message's author.
type MessageRole string

// Message roles.
const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// EnumValues lists the message roles.
func (MessageRole) EnumValues() []string { return []string{string(RoleUser), string(RoleAssistant)} }

// TextContent is one text part of a message.
type TextContent struct {
	Type string `json:"type" schema:"const=text"`
	Text string `json:"text"`
}
