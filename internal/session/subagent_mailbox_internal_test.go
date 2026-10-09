package session

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/subagent"
)

func TestMailboxBoundaryHidesInternalIdentities(t *testing.T) {
	t.Parallel()
	boundary, err := mailboxBoundary([]subagent.MailboxItem{{
		ID:             "mailbox_0123456789abcdef0123456789abcdef",
		Kind:           subagent.ParentDeliveryTask,
		ConversationID: "subagent_0123456789abcdef0123456789abcdef",
		TaskID:         "task_0123456789abcdef0123456789abcdef",
		AgentName:      "reviewer", State: subagent.TaskFailed,
		Error: "store subagent_0123456789abcdef0123456789abcdef failed at turn_0123456789abcdef0123456789abcdef",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, content := range boundary.Content {
		if value, ok := content.(droids.TextInput); ok {
			text += value.Text
		}
	}
	combined := text + string(boundary.Details)
	for _, identity := range []string{"subagent_", "task_", "turn_", "conversationId", "taskId"} {
		if strings.Contains(combined, identity) {
			t.Fatalf("model boundary retained %q: %s", identity, combined)
		}
	}
	for _, expected := range []string{"Subagent reviewer finished with state failed.", "Error: store <internal> failed at <internal>"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("boundary text missing %q: %s", expected, text)
		}
	}
}

func TestMailboxBoundaryRejectsMixedDeliveryKinds(t *testing.T) {
	_, err := mailboxBoundary([]subagent.MailboxItem{
		{Kind: subagent.ParentDeliveryTask},
		{Kind: subagent.ParentDeliveryRequest},
	})
	if err == nil || !strings.Contains(err.Error(), "mixed subagent mailbox delivery kinds") {
		t.Fatalf("mixed boundary = %v", err)
	}
}

func TestParentRequestReplyBoundaryUsesReceiptAndReply(t *testing.T) {
	item := subagent.MailboxItem{
		ID: "mail_0123456789abcdef0123456789abcdef", Kind: subagent.ParentDeliveryRequest, RequestID: "subrequest_0123456789abcdef0123456789abcdef",
		AgentName: "reviewer", State: subagent.TaskCompleted, Summary: "Verified the finding.",
	}
	boundary, err := mailboxBoundary([]subagent.MailboxItem{item})
	if err != nil {
		t.Fatal(err)
	}
	if boundary.Kind != "subagent_request_result" || boundary.ID != item.ID {
		t.Fatalf("boundary identity = %#v", boundary)
	}
	if text := boundary.Content[0].(droids.TextInput).Text; text != "Subagent reviewer replied to request subrequest_0123456789abcdef0123456789abcdef with state completed.\nResult: Verified the finding." {
		t.Fatalf("boundary content = %q", text)
	}
}
