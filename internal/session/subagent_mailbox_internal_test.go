package session

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/subagent"
)

func TestMailboxBoundaryHidesInternalIdentities(t *testing.T) {
	t.Parallel()
	boundary, err := mailboxBoundary([]subagent.MailboxItem{{
		ID:             "mailbox_0123456789abcdef0123456789abcdef",
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
