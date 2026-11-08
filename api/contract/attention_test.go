package contract

import (
	"encoding/json"
	"slices"
	"testing"
)

func attentionSession(id, updated string, activity *SessionInfoActivity) SessionInfo {
	return SessionInfo{
		ID: id, CWD: "/work", Model: "test/echo", ConfigurationRevision: 1,
		CreatedAt: "2026-03-23T12:00:00Z", UpdatedAt: updated, Activity: activity,
	}
}

func waiting(since string) *SessionInfoActivity {
	return &SessionInfoActivity{Status: SessionActivityAwaitingResponse, Since: since}
}

var running = &SessionInfoActivity{Status: SessionActivityRunning}

// attentionFixture lists sessions in the attention order: two waiting, the
// longest first, with a tie on since broken by ID; two running, the most
// recently updated first; and three idle, the most recently updated first,
// with a tie on updatedAt broken by ID.
func attentionFixture() []SessionInfo {
	return []SessionInfo{
		attentionSession("session_0000000000000000000000000000000c", "2026-03-23T12:30:00Z", waiting("2026-03-23T12:04:00Z")),
		attentionSession("session_0000000000000000000000000000000a", "2026-03-23T12:31:00Z", waiting("2026-03-23T12:10:00Z")),
		attentionSession("session_0000000000000000000000000000000b", "2026-03-23T12:31:00Z", waiting("2026-03-23T12:10:00Z")),
		attentionSession("session_0000000000000000000000000000000e", "2026-03-23T12:29:00Z", running),
		attentionSession("session_0000000000000000000000000000000d", "2026-03-23T12:20:00Z", running),
		attentionSession("session_0000000000000000000000000000000g", "2026-03-23T12:40:00Z", nil),
		attentionSession("session_0000000000000000000000000000000f", "2026-03-22T09:00:00Z", nil),
		attentionSession("session_0000000000000000000000000000000h", "2026-03-22T09:00:00Z", nil),
	}
}

func TestCompareAttentionPutsWaitingThenRunningThenIdle(t *testing.T) {
	t.Parallel()
	want := attentionFixture()
	shuffled := []SessionInfo{want[6], want[3], want[0], want[7], want[2], want[5], want[4], want[1]}
	slices.SortFunc(shuffled, CompareAttention)
	ids := func(sessions []SessionInfo) []string {
		out := make([]string, len(sessions))
		for index, session := range sessions {
			out[index] = session.ID
		}
		return out
	}
	if got, want := ids(shuffled), ids(want); !slices.Equal(got, want) {
		t.Fatalf("attention order =\n%v\nwant\n%v", got, want)
	}
	if CompareAttention(want[0], want[0]) != 0 {
		t.Fatal("CompareAttention() of a session with itself is not zero")
	}
}

func TestSessionListValidateChecksTheAttentionOrder(t *testing.T) {
	t.Parallel()
	ordered := attentionFixture()
	if err := (SessionList{Sessions: ordered, Order: SessionListOrderAttention}).Validate(); err != nil {
		t.Fatalf("Validate() attention list error = %v", err)
	}
	swapped := slices.Clone(ordered)
	swapped[3], swapped[5] = swapped[5], swapped[3]
	idle := attentionSession("session_0000000000000000000000000000000a", "2026-03-23T12:00:00Z", nil)
	for name, list := range map[string]SessionList{
		"out of attention order":     {Sessions: swapped, Order: SessionListOrderAttention},
		"activity without an order":  {Sessions: ordered},
		"unsupported order":          {Sessions: []SessionInfo{idle}, Order: "newest"},
		"waiting without since":      {Sessions: []SessionInfo{attentionSession(idle.ID, idle.UpdatedAt, &SessionInfoActivity{Status: SessionActivityAwaitingResponse})}, Order: SessionListOrderAttention},
		"running with since":         {Sessions: []SessionInfo{attentionSession(idle.ID, idle.UpdatedAt, &SessionInfoActivity{Status: SessionActivityRunning, Since: "2026-03-23T12:00:00Z"})}, Order: SessionListOrderAttention},
		"idle stated as an activity": {Sessions: []SessionInfo{attentionSession(idle.ID, idle.UpdatedAt, &SessionInfoActivity{Status: "idle"})}, Order: SessionListOrderAttention},
		"waiting with a bad since":   {Sessions: []SessionInfo{attentionSession(idle.ID, idle.UpdatedAt, waiting("yesterday"))}, Order: SessionListOrderAttention},
	} {
		if err := list.Validate(); err == nil {
			t.Errorf("Validate() accepted %s", name)
		}
	}
}

// A list without an order encodes exactly as it did before orders existed,
// so released clients that decode strictly read it unchanged.
func TestSessionListWithoutAnOrderEncodesNoOrderOrActivity(t *testing.T) {
	t.Parallel()
	list := SessionList{Sessions: []SessionInfo{attentionSession("session_0000000000000000000000000000000a", "2026-03-23T12:00:00Z", nil)}}
	encoded, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sessions":[{"id":"session_0000000000000000000000000000000a","cwd":"/work","model":"test/echo","thinkingLevel":"","configurationRevision":1,"createdAt":"2026-03-23T12:00:00Z","updatedAt":"2026-03-23T12:00:00Z"}]}`
	if string(encoded) != want {
		t.Fatalf("encoded list =\n%s\nwant\n%s", encoded, want)
	}
}
