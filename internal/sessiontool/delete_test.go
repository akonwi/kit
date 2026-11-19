package sessiontool

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
)

var deletionNow = time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

type fakeDeletion struct {
	sessions []SessionSummary
	// afterApproval replaces sessions once the approval is answered.
	afterApproval []SessionSummary
	blocked       map[string]error
	deleteErrors  map[string]error

	deleted []string
}

func (f *fakeDeletion) ModelSessions(context.Context) ([]SessionSummary, error) {
	return append([]SessionSummary(nil), f.sessions...), nil
}
func (f *fakeDeletion) DeletionBlocker(_ context.Context, id string) error { return f.blocked[id] }
func (f *fakeDeletion) DeleteModelSession(_ context.Context, id string) error {
	if err := f.deleteErrors[id]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, id)
	return nil
}

// deletionTree is an owner with two children; the first has a fork, which
// has a fork of its own. One unrelated session and a sibling of the owner
// complete the store.
func deletionTree() []SessionSummary {
	return []SessionSummary{
		{ID: "session_owner", Name: "Coordinator", CWD: "/work", Persistent: true},
		{ID: "session_docs", Name: "Docs", CWD: "/work/docs", ParentSessionID: "session_owner", Persistent: true, LastActivity: deletionNow.Add(-2 * time.Hour)},
		{ID: "session_docs_fork", Name: "Docs fork", CWD: "/work/docs", ParentSessionID: "session_docs", Persistent: true, LastActivity: deletionNow.Add(-3 * 24 * time.Hour)},
		{ID: "session_docs_fork_fork", Name: "", CWD: "/work/docs", ParentSessionID: "session_docs_fork", Persistent: true, LastActivity: deletionNow.Add(-90 * time.Second)},
		{ID: "session_api", Name: "API", CWD: "/work/api", ParentSessionID: "session_owner", Persistent: true, LastActivity: deletionNow.Add(-20 * time.Second)},
		{ID: "session_other", Name: "Other", CWD: "/elsewhere", Persistent: true},
		{ID: "session_other_child", Name: "Other child", CWD: "/elsewhere", ParentSessionID: "session_other", Persistent: true},
	}
}

var (
	approved = droids.Settlement{Status: droids.InteractionAnswered, Answer: droids.Answer{Confirmed: ptr(true)}}
	refused  = droids.Settlement{Status: droids.InteractionAnswered, Answer: droids.Answer{Confirmed: ptr(false)}}
)

func ptr[T any](value T) *T { return &value }

// deletion is what one call did, in the order the droid runs a tool that
// asks first: Confirm, then Declined or Execute once the approval settles.
type deletion struct {
	asked    *droids.Confirmation
	refusal  string
	response DeleteResponse
	result   droids.ToolResult
}

func runDelete(t *testing.T, fake *fakeDeletion, settlement droids.Settlement, ids ...string) deletion {
	t.Helper()
	tool := &ToolService{Deletion: fake, Now: func() time.Time { return deletionNow }}
	args := deleteArguments{SessionIDs: ids}
	confirmation, err := tool.confirmDeletion(t.Context(), "session_owner", args)
	if err != nil {
		return deletion{refusal: err.Error()}
	}
	if confirmation == nil {
		t.Fatal("the call ran without asking")
	}
	if fake.afterApproval != nil {
		fake.sessions = fake.afterApproval
	}
	var result droids.ToolResult
	if settlement.Status == droids.InteractionAnswered && *settlement.Answer.Confirmed {
		result = deleteResult(tool.executeDelete(t.Context(), "session_owner", args))
	} else {
		result = deleteResult(tool.declinedDeletion(t.Context(), "session_owner", args, settlement))
	}
	var response DeleteResponse
	text := result.Content[0].(droids.TextContent).Text
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("result text %q: %v", text, err)
	}
	if len(result.Details) == 0 {
		t.Fatal("result has no details")
	}
	return deletion{asked: confirmation, response: response, result: result}
}

func TestDeleteAsksOnceWithEverySessionItWouldDelete(t *testing.T) {
	got := runDelete(t, &fakeDeletion{sessions: deletionTree()}, approved, "session_docs", "session_api")
	want := droids.Confirmation{
		Title: "Delete 4 sessions?",
		Detail: "Docs · /work/docs · active 2 hours ago\n" +
			"↳ Docs fork · /work/docs · active 3 days ago\n" +
			"  ↳ Untitled session · /work/docs · active 1 minute ago\n" +
			"API · /work/api · active just now\n" +
			"\nThis can’t be undone.",
		ConfirmLabel: "Delete", CancelLabel: "Keep",
	}
	if got.asked == nil || *got.asked != want {
		t.Fatalf("asked = %#v\nwant %#v", got.asked, want)
	}
}

func TestDeleteNamesASingleSessionInTheApprovalTitle(t *testing.T) {
	got := runDelete(t, &fakeDeletion{sessions: deletionTree()}, refused, "session_api")
	if got.asked == nil || got.asked.Title != "Delete “API”?" || got.asked.Detail != "API · /work/api · active just now\n\nThis can’t be undone." {
		t.Fatalf("asked = %#v", got.asked)
	}
}

func TestDeleteRemovesDescendantsBeforeTheirAncestors(t *testing.T) {
	fake := &fakeDeletion{sessions: deletionTree()}
	got := runDelete(t, fake, approved, "session_docs", "session_api")
	wantDeleted := []string{"session_api", "session_docs_fork_fork", "session_docs_fork", "session_docs"}
	if !reflect.DeepEqual(fake.deleted, wantDeleted) {
		t.Fatalf("deleted = %v, want %v", fake.deleted, wantDeleted)
	}
	want := DeleteResponse{
		Deleted:    []DeletedSession{{ID: "session_api", Name: "API"}, {ID: "session_docs_fork_fork"}, {ID: "session_docs_fork", Name: "Docs fork"}, {ID: "session_docs", Name: "Docs"}},
		Resolution: "answered", Message: "Deleted 4 sessions.",
	}
	if got.result.IsError || !reflect.DeepEqual(got.response, want) {
		t.Fatalf("response = %#v (error %v)", got.response, got.result.IsError)
	}
}

func TestDeleteReportsADeclineAsAReadableResult(t *testing.T) {
	for _, tc := range []struct {
		settlement droids.Settlement
		resolution string
		message    string
	}{
		{refused, "negative_confirmation", "The user declined. Nothing was deleted."},
		{droids.Settlement{Status: droids.InteractionDeclined, Reason: "the user declined"}, "user_cancelled", "The user declined. Nothing was deleted."},
		{droids.Settlement{Status: droids.InteractionCancelled, Reason: "the turn ended"}, "run_abort", "The approval ended without an answer (run_abort). Nothing was deleted."},
		{droids.Settlement{Status: droids.InteractionUnavailable, Reason: "unavailable"}, "unavailable", "The approval ended without an answer (unavailable). Nothing was deleted."},
	} {
		t.Run(tc.resolution, func(t *testing.T) {
			fake := &fakeDeletion{sessions: deletionTree()}
			got := runDelete(t, fake, tc.settlement, "session_docs", "session_gone")
			want := DeleteResponse{
				Declined: true, Resolution: tc.resolution, Message: tc.message,
				Failed: []FailedDeletion{{ID: "session_gone", Error: "this session has no child with this ID"}},
			}
			if got.result.IsError || !reflect.DeepEqual(got.response, want) || len(fake.deleted) != 0 {
				t.Fatalf("response = %#v, error = %v, deleted = %v", got.response, got.result.IsError, fake.deleted)
			}
		})
	}
}

func TestDeleteReportsEveryIDThatIsNotAChildAlike(t *testing.T) {
	fake := &fakeDeletion{sessions: deletionTree()}
	got := runDelete(t, fake, approved, "session_owner", "session_docs_fork", "session_other", "session_other_child", "session_missing", "session_api")
	if got.asked == nil || got.asked.Title != "Delete “API”?" {
		t.Fatalf("asked = %#v", got.asked)
	}
	reason := "this session has no child with this ID"
	want := DeleteResponse{
		Deleted: []DeletedSession{{ID: "session_api", Name: "API"}},
		Failed: []FailedDeletion{
			{ID: "session_owner", Error: reason}, {ID: "session_docs_fork", Error: reason}, {ID: "session_other", Error: reason},
			{ID: "session_other_child", Error: reason}, {ID: "session_missing", Error: reason},
		},
		Resolution: "answered", Message: "Deleted 1 session; 5 not deleted.",
	}
	if got.result.IsError || !reflect.DeepEqual(got.response, want) || !reflect.DeepEqual(fake.deleted, []string{"session_api"}) {
		t.Fatalf("response = %#v, deleted = %v", got.response, fake.deleted)
	}
}

func TestDeleteReportsMissingSessionsAndDeletesTheRest(t *testing.T) {
	got := runDelete(t, &fakeDeletion{sessions: deletionTree()}, approved, "session_gone", "session_api")
	if got.asked == nil || got.asked.Title != "Delete “API”?" {
		t.Fatalf("asked = %#v", got.asked)
	}
	want := DeleteResponse{
		Deleted:    []DeletedSession{{ID: "session_api", Name: "API"}},
		Failed:     []FailedDeletion{{ID: "session_gone", Error: "this session has no child with this ID"}},
		Resolution: "answered", Message: "Deleted 1 session; 1 not deleted.",
	}
	if got.result.IsError || !reflect.DeepEqual(got.response, want) {
		t.Fatalf("response = %#v", got.response)
	}
}

func TestDeleteRefusesWithoutAskingWhenEverySessionIsMissing(t *testing.T) {
	got := runDelete(t, &fakeDeletion{sessions: deletionTree()}, approved, "session_gone", "session_lost")
	if got.refusal != "nothing was deleted: this session has no child with this ID: session_gone, session_lost" {
		t.Fatalf("got %#v", got)
	}
}

func TestDeleteRefusesAnOwnerThatHasAParent(t *testing.T) {
	sessions := deletionTree()
	sessions[0].ParentSessionID = "session_other"
	got := runDelete(t, &fakeDeletion{sessions: sessions}, approved, "session_docs")
	if got.refusal != "nothing was deleted: only a top-level persistent session can delete its child sessions" {
		t.Fatalf("got %#v", got)
	}
}

func TestDeleteRefusesWithoutAskingWhenAnyDescendantIsBusy(t *testing.T) {
	fake := &fakeDeletion{sessions: deletionTree(), blocked: map[string]error{
		"session_docs_fork": errors.New("session cannot be deleted while work is active: it is running a turn"),
	}}
	got := runDelete(t, fake, approved, "session_docs", "session_api")
	want := "nothing was deleted: Docs fork (session_docs_fork): session cannot be deleted while work is active: it is running a turn"
	if got.refusal != want || len(fake.deleted) != 0 {
		t.Fatalf("got %#v, deleted = %v", got, fake.deleted)
	}
}

func TestDeleteKeepsAncestorsOfASessionItCouldNotDelete(t *testing.T) {
	fake := &fakeDeletion{sessions: deletionTree(), deleteErrors: map[string]error{
		"session_docs_fork_fork": errors.New("disk full"),
	}}
	got := runDelete(t, fake, approved, "session_docs", "session_api")
	want := DeleteResponse{
		Deleted: []DeletedSession{{ID: "session_api", Name: "API"}},
		Failed: []FailedDeletion{
			{ID: "session_docs", Name: "Docs", Error: "a session below it was not deleted"},
			{ID: "session_docs_fork", Name: "Docs fork", Error: "a session below it was not deleted"},
			{ID: "session_docs_fork_fork", Error: "disk full"},
		},
		Resolution: "answered", Message: "Deleted 1 session; 3 not deleted.",
	}
	if got.result.IsError || !reflect.DeepEqual(got.response, want) || !reflect.DeepEqual(fake.deleted, []string{"session_api"}) {
		t.Fatalf("response = %#v, deleted = %v", got.response, fake.deleted)
	}
}

func TestDeleteRechecksOwnershipAfterApproval(t *testing.T) {
	// Approving a child covers its descendants as they are when it runs.
	after := deletionTree()
	after = append(after, SessionSummary{ID: "session_docs_new_fork", Name: "New fork", CWD: "/work/docs", ParentSessionID: "session_docs", Persistent: true})
	fake := &fakeDeletion{sessions: deletionTree(), afterApproval: after}
	got := runDelete(t, fake, approved, "session_docs")
	wantDeleted := []string{"session_docs_new_fork", "session_docs_fork_fork", "session_docs_fork", "session_docs"}
	if got.result.IsError || !reflect.DeepEqual(fake.deleted, wantDeleted) {
		t.Fatalf("response = %#v, deleted = %v, want %v", got.response, fake.deleted, wantDeleted)
	}

	// A session that stopped being a child is not deleted.
	moved := deletionTree()
	moved[4].ParentSessionID = ""
	fake = &fakeDeletion{sessions: deletionTree(), afterApproval: moved}
	got = runDelete(t, fake, approved, "session_api")
	want := DeleteResponse{
		Failed:     []FailedDeletion{{ID: "session_api", Error: "this session has no child with this ID"}},
		Resolution: "answered", Error: "nothing was deleted",
	}
	if !got.result.IsError || !reflect.DeepEqual(got.response, want) || len(fake.deleted) != 0 {
		t.Fatalf("response = %#v, deleted = %v", got.response, fake.deleted)
	}
}

func TestDeleteValidatesRequestedIDs(t *testing.T) {
	tooMany := make([]string, maxDeleteRequestIDs+1)
	for i := range tooMany {
		tooMany[i] = "session_api"
	}
	for _, tc := range []struct {
		ids  []string
		want string
	}{
		{nil, "sessionIds must name at least one session"},
		{[]string{" "}, "sessionIds must not contain empty IDs"},
		{tooMany, "sessionIds may name at most 20 sessions"},
		{[]string{"session_api", strings.Repeat("x", 10000)}, "sessionIds must contain only session IDs"},
		{[]string{"session_API"}, "sessionIds must contain only session IDs"},
	} {
		if got := runDelete(t, &fakeDeletion{sessions: deletionTree()}, approved, tc.ids...); got.refusal != tc.want {
			t.Fatalf("ids %v: got %#v", tc.ids, got)
		}
	}
}

func TestDeleteRefusesMoreSessionsThanOneApprovalCovers(t *testing.T) {
	sessions := []SessionSummary{{ID: "session_owner", Persistent: true}, {ID: "session_root", ParentSessionID: "session_owner", Persistent: true}}
	for i := 0; i < maxDeletedSessions; i++ {
		sessions = append(sessions, SessionSummary{ID: "session_fork_" + strings.Repeat("x", i+1), ParentSessionID: "session_root", Persistent: true})
	}
	got := runDelete(t, &fakeDeletion{sessions: sessions}, approved, "session_root")
	if got.refusal != "nothing was deleted: one call can delete at most 50 sessions, descendants included" {
		t.Fatalf("got %#v", got)
	}
}

func TestToolsIncludeDeleteOnlyWithADeletionService(t *testing.T) {
	names := func(tools []droids.AnyTool) []string {
		var result []string
		for _, tool := range tools {
			result = append(result, tool.Name())
		}
		return result
	}
	tools, err := (&ToolService{Service: &fakeService{}}).Tools("session_owner")
	if err != nil || !reflect.DeepEqual(names(tools), []string{ToolName}) {
		t.Fatalf("tools = %v, %v", names(tools), err)
	}
	tools, err = (&ToolService{Service: &fakeService{}, Deletion: &fakeDeletion{}}).Tools("session_owner")
	if err != nil || !reflect.DeepEqual(names(tools), []string{ToolName, DeleteToolName}) {
		t.Fatalf("tools = %v, %v", names(tools), err)
	}
}

func TestDeleteAnswersAnUnrelatedSessionAsIfItDidNotExist(t *testing.T) {
	unrelated := runDelete(t, &fakeDeletion{sessions: deletionTree()}, approved, "session_other")
	missing := runDelete(t, &fakeDeletion{sessions: deletionTree()}, approved, "session_gone")
	if strings.ReplaceAll(unrelated.refusal, "session_other", "session_gone") != missing.refusal || missing.refusal == "" {
		t.Fatalf("unrelated = %q\nmissing = %q", unrelated.refusal, missing.refusal)
	}
}
