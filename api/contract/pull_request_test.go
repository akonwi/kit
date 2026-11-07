package contract

import (
	"strings"
	"testing"
)

func validPullRequestStatus() SessionPullRequestStatus {
	return SessionPullRequestStatus{
		SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo", Branch: "main",
		PullRequest: &PullRequestStatus{Number: 42, URL: "https://github.com/a/b/pull/42", State: PullRequestOpen, Checks: PullRequestChecks{
			State: PullRequestChecksFailing, Required: PullRequestRequirementUnsatisfied,
			Counts: PullRequestCheckCounts{Passed: 1, Failed: 1},
			Items:  []PullRequestCheck{{Name: "test", Workflow: "CI", State: PullRequestCheckFailed, Required: true}, {Name: "lint", State: PullRequestCheckPassed}},
		}},
	}
}

func TestSessionPullRequestStatusValidation(t *testing.T) {
	valid := validPullRequestStatus()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, value := range []SessionPullRequestStatus{
		{SessionID: valid.SessionID, CWD: "/repo"},
		{SessionID: valid.SessionID, CWD: "/repo", Branch: "main"},
	} {
		if err := value.Validate(); err != nil {
			t.Fatalf("%+v: %v", value, err)
		}
	}
	atBound := validPullRequestStatus()
	atBound.PullRequest.Checks = PullRequestChecks{State: PullRequestChecksPassing, Required: PullRequestRequirementSatisfied, Counts: PullRequestCheckCounts{Passed: MaxPullRequestChecks + 3}, Omitted: 3}
	for range MaxPullRequestChecks {
		atBound.PullRequest.Checks.Items = append(atBound.PullRequest.Checks.Items, PullRequestCheck{Name: strings.Repeat("é", MaxPullRequestCheckTextBytes/2), Workflow: strings.Repeat("w", MaxPullRequestCheckTextBytes), State: PullRequestCheckPassed})
	}
	if err := atBound.Validate(); err != nil {
		t.Fatalf("bounded checks rejected: %v", err)
	}
	for name, mutate := range map[string]func(*SessionPullRequestStatus){
		"bad session":            func(v *SessionPullRequestStatus) { v.SessionID = "session_x" },
		"relative cwd":           func(v *SessionPullRequestStatus) { v.CWD = "repo" },
		"control in branch":      func(v *SessionPullRequestStatus) { v.Branch = "main\n" },
		"pull request no branch": func(v *SessionPullRequestStatus) { v.Branch = "" },
		"zero number":            func(v *SessionPullRequestStatus) { v.PullRequest.Number = 0 },
		"unsafe url":             func(v *SessionPullRequestStatus) { v.PullRequest.URL = "javascript:alert(1)" },
		"credential url":         func(v *SessionPullRequestStatus) { v.PullRequest.URL = "https://user:pass@github.com/a/b/pull/42" },
		"unknown state":          func(v *SessionPullRequestStatus) { v.PullRequest.State = "OPEN" },
		"unknown checks state":   func(v *SessionPullRequestStatus) { v.PullRequest.Checks.State = "green" },
		"state disagrees":        func(v *SessionPullRequestStatus) { v.PullRequest.Checks.State = PullRequestChecksPassing },
		"unknown requirement":    func(v *SessionPullRequestStatus) { v.PullRequest.Checks.Required = "met" },
		"null items":             func(v *SessionPullRequestStatus) { v.PullRequest.Checks.Items = nil },
		"negative count":         func(v *SessionPullRequestStatus) { v.PullRequest.Checks.Counts.Skipped = -1 },
		"counts disagree":        func(v *SessionPullRequestStatus) { v.PullRequest.Checks.Counts.Passed = 2 },
		"omitted below bound": func(v *SessionPullRequestStatus) {
			v.PullRequest.Checks.Omitted = 1
			v.PullRequest.Checks.Counts.Passed = 2
		},
		"listed beyond count": func(v *SessionPullRequestStatus) {
			v.PullRequest.Checks.Items[1].State = PullRequestCheckFailed
			v.PullRequest.Checks.Counts = PullRequestCheckCounts{Failed: 1, Pending: 1}
		},
		"empty name": func(v *SessionPullRequestStatus) { v.PullRequest.Checks.Items[0].Name = "" },
		"long name": func(v *SessionPullRequestStatus) {
			v.PullRequest.Checks.Items[0].Name = strings.Repeat("n", MaxPullRequestCheckTextBytes+1)
		},
		"format in name": func(v *SessionPullRequestStatus) { v.PullRequest.Checks.Items[0].Name = "te\u202est" },
		"long workflow": func(v *SessionPullRequestStatus) {
			v.PullRequest.Checks.Items[0].Workflow = strings.Repeat("w", MaxPullRequestCheckTextBytes+1)
		},
		"unknown check": func(v *SessionPullRequestStatus) { v.PullRequest.Checks.Items[0].State = "success" },
	} {
		value := validPullRequestStatus()
		mutate(&value)
		if value.Validate() == nil {
			t.Errorf("%s: accepted %+v", name, value)
		}
	}
	tooMany := atBound
	tooMany.PullRequest = &PullRequestStatus{Number: 1, URL: "https://github.com/a/b/pull/1", State: PullRequestOpen, Checks: atBound.PullRequest.Checks}
	tooMany.PullRequest.Checks.Items = append(append([]PullRequestCheck(nil), tooMany.PullRequest.Checks.Items...), PullRequestCheck{Name: "extra", State: PullRequestCheckPassed})
	tooMany.PullRequest.Checks.Omitted = 2
	if tooMany.Validate() == nil {
		t.Error("accepted more than the listed-check bound")
	}
}
