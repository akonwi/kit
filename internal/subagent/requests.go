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

// InboxDelivery is one request or reply delivered to a child conversation's
// inbox: the model-facing text and the parts a client renders.
type InboxDelivery struct {
	// Text is the model-facing message, including the receipt and instructions.
	Text string
	// From names the other agent: the sender of a request or the recipient
	// that replied.
	From string
	// Message is the request or reply body, empty when the request failed.
	Message string
	// Failure explains why a request failed; it is set only for failed replies.
	Failure string
}

// DeliverToInbox describes request delivered to a child as origin, which is
// TaskOriginRequest for the recipient or TaskOriginReply for the sender.
func DeliverToInbox(request Request, origin TaskOrigin) InboxDelivery {
	if origin != TaskOriginReply {
		return InboxDelivery{
			Text: "Subagent request " + request.ID + " from " + request.SenderName + ":\n" + request.Message + "\nUse subagent_reply with this receipt to answer.",
			From: request.SenderName, Message: request.Message,
		}
	}
	if request.State != RequestReplied {
		return InboxDelivery{
			Text: "Subagent request " + request.ID + " failed: " + request.Failure,
			From: request.RecipientName, Failure: request.Failure,
		}
	}
	return InboxDelivery{
		Text: "Subagent reply to " + request.ID + " from " + request.RecipientName + ":\n" + request.Reply,
		From: request.RecipientName, Message: request.Reply,
	}
}
