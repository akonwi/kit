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
	approval      Approval
	approvalErr   error

	asked   []Confirmation
	call    droids.ToolContext
	deleted []string
}

func (f *fakeDeletion) ModelSessions(context.Context) ([]SessionSummary, error) {
	return append([]SessionSummary(nil), f.sessions...), nil
}
func (f *fakeDeletion) DeletionBlocker(_ context.Context, id string) error { return f.blocked[id] }
func (f *fakeDeletion) ConfirmToolCall(_ context.Context, owner string, call droids.ToolContext, confirmation Confirmation) (Approval, error) {
	if owner != "session_owner" {
		return Approval{}, errors.New("asked the wrong session")
	}
	f.asked = append(f.asked, confirmation)
	f.call = call
	if f.afterApproval != nil {
		f.sessions = f.afterApproval
	}
	return f.approval, f.approvalErr
}
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

func runDelete(t *testing.T, deletion *fakeDeletion, ids ...string) (DeleteResponse, droids.ToolResult) {
	t.Helper()
	tool := &ToolService{Deletion: deletion, Now: func() time.Time { return deletionNow }}
	call := droids.ToolContext{TurnID: "turn_one", ToolCallID: "call_one"}
	result := deleteResult(tool.executeDelete(t.Context(), "session_owner", call, deleteArguments{SessionIDs: ids}))
	var response DeleteResponse
	text := result.Content[0].(droids.TextContent).Text
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatalf("result text %q: %v", text, err)
	}
	if len(result.Details) == 0 {
		t.Fatal("result has no details")
	}
	return response, result
}

func TestDeleteAsksOnceWithEverySessionItWouldDelete(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree(), approval: Approval{Confirmed: true, Resolution: "answered"}}
	runDelete(t, deletion, "session_docs", "session_api")
	want := []Confirmation{{
		Title: "Delete 4 sessions?",
		Detail: "Docs · /work/docs · active 2 hours ago\n" +
			"↳ Docs fork · /work/docs · active 3 days ago\n" +
			"  ↳ Untitled session · /work/docs · active 1 minute ago\n" +
			"API · /work/api · active just now\n" +
			"\nThis can’t be undone.",
		ConfirmLabel: "Delete", CancelLabel: "Keep",
	}}
	if !reflect.DeepEqual(deletion.asked, want) {
		t.Fatalf("asked = %#v\nwant %#v", deletion.asked, want)
	}
	if deletion.call != (droids.ToolContext{TurnID: "turn_one", ToolCallID: "call_one"}) {
		t.Fatalf("call = %+v", deletion.call)
	}
}

func TestDeleteNamesASingleSessionInTheApprovalTitle(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree(), approval: Approval{Confirmed: false, Resolution: "negative_confirmation"}}
	runDelete(t, deletion, "session_api")
	if len(deletion.asked) != 1 || deletion.asked[0].Title != "Delete “API”?" || deletion.asked[0].Detail != "API · /work/api · active just now\n\nThis can’t be undone." {
		t.Fatalf("asked = %#v", deletion.asked)
	}
}

func TestDeleteRemovesDescendantsBeforeTheirAncestors(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree(), approval: Approval{Confirmed: true, Resolution: "answered"}}
	response, result := runDelete(t, deletion, "session_docs", "session_api")
	wantDeleted := []string{"session_api", "session_docs_fork_fork", "session_docs_fork", "session_docs"}
	if !reflect.DeepEqual(deletion.deleted, wantDeleted) {
		t.Fatalf("deleted = %v, want %v", deletion.deleted, wantDeleted)
	}
	want := DeleteResponse{
		Deleted:    []DeletedSession{{ID: "session_api", Name: "API"}, {ID: "session_docs_fork_fork"}, {ID: "session_docs_fork", Name: "Docs fork"}, {ID: "session_docs", Name: "Docs"}},
		Resolution: "answered", Message: "Deleted 4 sessions.",
	}
	if result.IsError || !reflect.DeepEqual(response, want) {
		t.Fatalf("response = %#v (error %v)", response, result.IsError)
	}
}

func TestDeleteReportsADeclineAsAReadableResult(t *testing.T) {
	for _, tc := range []struct {
		resolution string
		message    string
	}{
		{"negative_confirmation", "The user declined. Nothing was deleted."},
		{"user_cancelled", "The user declined. Nothing was deleted."},
		{"run_abort", "The approval ended without an answer (run_abort). Nothing was deleted."},
		{"unavailable", "The approval ended without an answer (unavailable). Nothing was deleted."},
	} {
		t.Run(tc.resolution, func(t *testing.T) {
			deletion := &fakeDeletion{sessions: deletionTree(), approval: Approval{Resolution: tc.resolution}}
			response, result := runDelete(t, deletion, "session_docs")
			want := DeleteResponse{Declined: true, Resolution: tc.resolution, Message: tc.message}
			if result.IsError || !reflect.DeepEqual(response, want) || len(deletion.deleted) != 0 {
				t.Fatalf("response = %#v, error = %v, deleted = %v", response, result.IsError, deletion.deleted)
			}
		})
	}
}

func TestDeleteReportsEveryIDThatIsNotAChildAlike(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree(), approval: Approval{Confirmed: true, Resolution: "answered"}}
	response, result := runDelete(t, deletion, "session_owner", "session_docs_fork", "session_other", "session_other_child", "session_missing", "session_api")
	if len(deletion.asked) != 1 || deletion.asked[0].Title != "Delete “API”?" {
		t.Fatalf("asked = %#v", deletion.asked)
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
	if result.IsError || !reflect.DeepEqual(response, want) || !reflect.DeepEqual(deletion.deleted, []string{"session_api"}) {
		t.Fatalf("response = %#v, deleted = %v", response, deletion.deleted)
	}
}

func TestDeleteReportsMissingSessionsAndDeletesTheRest(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree(), approval: Approval{Confirmed: true, Resolution: "answered"}}
	response, result := runDelete(t, deletion, "session_gone", "session_api")
	if len(deletion.asked) != 1 || deletion.asked[0].Title != "Delete “API”?" {
		t.Fatalf("asked = %#v", deletion.asked)
	}
	want := DeleteResponse{
		Deleted:    []DeletedSession{{ID: "session_api", Name: "API"}},
		Failed:     []FailedDeletion{{ID: "session_gone", Error: "this session has no child with this ID"}},
		Resolution: "answered", Message: "Deleted 1 session; 1 not deleted.",
	}
	if result.IsError || !reflect.DeepEqual(response, want) {
		t.Fatalf("response = %#v", response)
	}
}

func TestDeleteFailsWithoutAskingWhenEverySessionIsMissing(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree(), approval: Approval{Confirmed: true}}
	response, result := runDelete(t, deletion, "session_gone", "session_lost")
	want := DeleteResponse{
		Failed: []FailedDeletion{{ID: "session_gone", Error: "this session has no child with this ID"}, {ID: "session_lost", Error: "this session has no child with this ID"}},
		Error:  "nothing was deleted",
	}
	if !result.IsError || !reflect.DeepEqual(response, want) || len(deletion.asked) != 0 {
		t.Fatalf("response = %#v, asked = %v", response, deletion.asked)
	}
}

func TestDeleteRefusesAnOwnerThatHasAParent(t *testing.T) {
	sessions := deletionTree()
	sessions[0].ParentSessionID = "session_other"
	deletion := &fakeDeletion{sessions: sessions, approval: Approval{Confirmed: true}}
	response, result := runDelete(t, deletion, "session_docs")
	if !result.IsError || response.Error != "nothing was deleted: only a top-level persistent session can delete its child sessions" || len(deletion.asked) != 0 {
		t.Fatalf("response = %#v, asked = %v", response, deletion.asked)
	}
}

func TestDeleteRefusesWithoutAskingWhenAnyDescendantIsBusy(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree(), approval: Approval{Confirmed: true}, blocked: map[string]error{
		"session_docs_fork": errors.New("session cannot be deleted while work is active: it is running a turn"),
	}}
	response, result := runDelete(t, deletion, "session_docs", "session_api")
	want := "nothing was deleted: Docs fork (session_docs_fork): session cannot be deleted while work is active: it is running a turn"
	if !result.IsError || response.Error != want || len(deletion.asked) != 0 || len(deletion.deleted) != 0 {
		t.Fatalf("response = %#v, asked = %v, deleted = %v", response, deletion.asked, deletion.deleted)
	}
}

func TestDeleteKeepsAncestorsOfASessionItCouldNotDelete(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree(), approval: Approval{Confirmed: true, Resolution: "answered"}, deleteErrors: map[string]error{
		"session_docs_fork_fork": errors.New("disk full"),
	}}
	response, result := runDelete(t, deletion, "session_docs", "session_api")
	want := DeleteResponse{
		Deleted: []DeletedSession{{ID: "session_api", Name: "API"}},
		Failed: []FailedDeletion{
			{ID: "session_docs", Name: "Docs", Error: "a session below it was not deleted"},
			{ID: "session_docs_fork", Name: "Docs fork", Error: "a session below it was not deleted"},
			{ID: "session_docs_fork_fork", Error: "disk full"},
		},
		Resolution: "answered", Message: "Deleted 1 session; 3 not deleted.",
	}
	if result.IsError || !reflect.DeepEqual(response, want) || !reflect.DeepEqual(deletion.deleted, []string{"session_api"}) {
		t.Fatalf("response = %#v, deleted = %v", response, deletion.deleted)
	}
}

func TestDeleteRechecksOwnershipAfterApproval(t *testing.T) {
	after := deletionTree()
	after = append(after, SessionSummary{ID: "session_docs_new_fork", Name: "New fork", CWD: "/work/docs", ParentSessionID: "session_docs", Persistent: true})
	deletion := &fakeDeletion{sessions: deletionTree(), afterApproval: after, approval: Approval{Confirmed: true, Resolution: "answered"}}
	response, result := runDelete(t, deletion, "session_docs")
	if !result.IsError || response.Error != "nothing was deleted: New fork (session_docs_new_fork) was created after the approval" || len(deletion.deleted) != 0 {
		t.Fatalf("response = %#v, deleted = %v", response, deletion.deleted)
	}

	moved := deletionTree()
	moved[4].ParentSessionID = ""
	deletion = &fakeDeletion{sessions: deletionTree(), afterApproval: moved, approval: Approval{Confirmed: true, Resolution: "answered"}}
	response, result = runDelete(t, deletion, "session_api")
	want := DeleteResponse{
		Failed:     []FailedDeletion{{ID: "session_api", Name: "API", Error: "it no longer exists or is no longer a child of this session"}},
		Resolution: "answered", Error: "nothing was deleted",
	}
	if !result.IsError || !reflect.DeepEqual(response, want) || len(deletion.deleted) != 0 {
		t.Fatalf("response = %#v, deleted = %v", response, deletion.deleted)
	}
}

func TestDeleteReportsAnApprovalThatCouldNotBeAsked(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree(), approvalErr: errors.New("session has too many pending interactions")}
	response, result := runDelete(t, deletion, "session_api")
	if !result.IsError || response.Error != "nothing was deleted: session has too many pending interactions" || len(deletion.deleted) != 0 {
		t.Fatalf("response = %#v", response)
	}
}

func TestDeleteValidatesRequestedIDs(t *testing.T) {
	deletion := &fakeDeletion{sessions: deletionTree()}
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
		response, result := runDelete(t, deletion, tc.ids...)
		if !result.IsError || response.Error != tc.want {
			t.Fatalf("ids %v: response = %#v", tc.ids, response)
		}
	}
	if len(deletion.asked) != 0 {
		t.Fatalf("asked = %v", deletion.asked)
	}
}

func TestDeleteRefusesMoreSessionsThanOneApprovalCovers(t *testing.T) {
	sessions := []SessionSummary{{ID: "session_owner", Persistent: true}, {ID: "session_root", ParentSessionID: "session_owner", Persistent: true}}
	for i := 0; i < maxDeletedSessions; i++ {
		sessions = append(sessions, SessionSummary{ID: "session_fork_" + strings.Repeat("x", i+1), ParentSessionID: "session_root", Persistent: true})
	}
	deletion := &fakeDeletion{sessions: sessions}
	response, _ := runDelete(t, deletion, "session_root")
	if response.Error != "nothing was deleted: one call can delete at most 50 sessions, descendants included" || len(deletion.asked) != 0 {
		t.Fatalf("response = %#v", response)
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
	unrelated, unrelatedResult := runDelete(t, &fakeDeletion{sessions: deletionTree()}, "session_other")
	missing, missingResult := runDelete(t, &fakeDeletion{sessions: deletionTree()}, "session_gone")
	unrelated.Failed[0].ID, unrelatedResult.Content = "session_gone", nil
	missingResult.Content = nil
	if !reflect.DeepEqual(unrelated, missing) || unrelatedResult.IsError != missingResult.IsError {
		t.Fatalf("unrelated = %#v\nmissing = %#v", unrelated, missing)
	}
}
