package pluginprotocol

import "github.com/akonwi/kit/api/contract"

// SessionStatus is a session's model configuration, context use, and
// cumulative usage, as clients' session details show them.
type SessionStatus struct {
	SessionID     string                `json:"sessionId" schema:"minLength=1"`
	Model         string                `json:"model" schema:"minLength=1" doc:"The provider/id model selector."`
	ThinkingLevel string                `json:"thinkingLevel" doc:"The applied thinking level."`
	Parent        *ParentSession        `json:"parent" schema:"nullable" doc:"The session this one was started from, such as a subagent's parent; null for none."`
	Context       *ContextStatus        `json:"context" schema:"nullable" doc:"Null before Kit knows the model's context window."`
	Usage         contract.SessionUsage `json:"usage" doc:"Cumulative provider usage and cost in US dollars, for this session only."`
}

// ParentSession identifies the session a session was started from.
type ParentSession struct {
	ID   string  `json:"id" schema:"minLength=1"`
	Name *string `json:"name" schema:"nullable"`
}

// ContextStatus is the estimated input of the next model request and the
// model's context window, in tokens.
type ContextStatus struct {
	Tokens int `json:"tokens" schema:"minimum=0"`
	Window int `json:"window" schema:"minimum=1"`
}

// CompactionParams identify a compaction of a session's conversation.
type CompactionParams struct {
	SessionID    string  `json:"sessionId" schema:"minLength=1"`
	CompactionID string  `json:"compactionId" schema:"minLength=1"`
	TurnID       *string `json:"turnId" schema:"nullable" doc:"Null for a compaction outside a turn, such as one a client asked for."`
}

// CompactionCompletedParams report how a compaction ended.
type CompactionCompletedParams struct {
	SessionID     string                            `json:"sessionId" schema:"minLength=1"`
	CompactionID  string                            `json:"compactionId" schema:"minLength=1"`
	TurnID        *string                           `json:"turnId" schema:"nullable" doc:"Null for a compaction outside a turn, such as one a client asked for."`
	FailureReason *contract.CompactionFailureReason `json:"failureReason" schema:"nullable" doc:"Null when compaction succeeded."`
}
