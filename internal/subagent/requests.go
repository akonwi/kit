package subagent

import "time"

// RequestState is the durable reply lifecycle of a subagent request.
type RequestState string

const (
	RequestOpen    RequestState = "open"
	RequestReplied RequestState = "replied"
	RequestFailed  RequestState = "failed"
	RequestExpired RequestState = "expired"
)

// Request records a reply-capable message addressed to a child conversation.
type Request struct {
	ID                      string
	OwnerSessionID          string
	SenderKind              string
	SenderConversationID    ConversationID
	RecipientConversationID ConversationID
	RecipientName           string
	SenderName              string
	SenderIdentity          string
	CallIdentity            string
	DeliveryMode            string
	Message                 string
	State                   RequestState
	Reply                   string
	Failure                 string
	DeliveryPending         bool // parent send result has not reached the parent mailbox
	CreatedAt               time.Time
	DeadlineAt              time.Time
	ResolvedAt              *time.Time
}

// RequestAdmission is the server-bound authority for one outgoing request.
type RequestAdmission struct {
	OwnerSessionID       string
	SenderConversationID ConversationID // empty only for a parent sender
	RecipientName        string
	// NewRecipient is an owner-authorized definition sampled by the host when
	// a parent or child addresses a configured sibling that has not started.
	NewRecipient  *Definition
	CWD           string
	Model         string
	ThinkingLevel string
	CallIdentity  string // durable sender tool-call identity
	DeliveryMode  string // ask or send; ask is parent-only
	Message       string
	Now           time.Time
	Deadline      time.Time
}
