package pullrequest

import (
	"reflect"
	"strings"
	"testing"
)

func TestSummarizeChecksDerivesTheOverallState(t *testing.T) {
	for _, test := range []struct {
		name   string
		states []CheckState
		want   ChecksState
	}{
		{"no checks", nil, ChecksNone},
		{"only skipped or unknown", []CheckState{CheckSkipped, CheckUnknown}, ChecksNone},
		{"all passed", []CheckState{CheckPassed, CheckSkipped}, ChecksPassing},
		{"one running", []CheckState{CheckPassed, CheckPending}, ChecksPending},
		{"one failed while others run", []CheckState{CheckPending, CheckFailed, CheckPassed}, ChecksFailing},
		{"one canceled", []CheckState{CheckPassed, CheckCanceled}, ChecksFailing},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := &Status{RequiredChecks: RequirementPending}
			for index, state := range test.states {
				status.Checks = append(status.Checks, Check{Name: string(rune('a' + index)), State: state})
			}
			got := SummarizeChecks(status)
			if got.State != test.want || got.Required != RequirementPending || got.Counts.Total() != len(test.states) {
				t.Fatalf("SummarizeChecks() = %+v, want state %s, required pending, %d counted", got, test.want, len(test.states))
			}
		})
	}
}

func TestSummarizeChecksCountsEveryStateAndListsUrgentChecksFirst(t *testing.T) {
	status := &Status{RequiredChecks: RequirementUnsatisfied, Checks: []Check{
		{Name: "build", Workflow: "CI", State: CheckPassed, Required: true},
		{Name: "test", Workflow: "CI", State: CheckFailed, Required: true},
		{Name: "docs", State: CheckSkipped},
		{Name: "lint", Workflow: "CI", State: CheckPending},
		{Name: "deploy", State: CheckCanceled},
		{Name: "legacy", State: CheckState("mystery")},
	}}
	got := SummarizeChecks(status)
	want := ChecksSummary{
		State: ChecksFailing, Required: RequirementUnsatisfied,
		Counts: CheckCounts{Passed: 1, Failed: 1, Pending: 1, Skipped: 1, Canceled: 1, Unknown: 1},
		Checks: []Check{
			{Name: "test", Workflow: "CI", State: CheckFailed, Required: true},
			{Name: "deploy", State: CheckCanceled},
			{Name: "lint", Workflow: "CI", State: CheckPending},
			{Name: "legacy", State: CheckUnknown},
			{Name: "build", Workflow: "CI", State: CheckPassed, Required: true},
			{Name: "docs", State: CheckSkipped},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SummarizeChecks() =\n%+v\nwant\n%+v", got, want)
	}
	if status.Checks[0].Name != "build" || status.Checks[5].State != CheckState("mystery") {
		t.Fatalf("SummarizeChecks() mutated its input: %+v", status.Checks)
	}
}

func TestSummarizeChecksBoundsTheListAndItsText(t *testing.T) {
	status := &Status{}
	for index := 0; index < MaxSummaryChecks+7; index++ {
		status.Checks = append(status.Checks, Check{Name: "check", State: CheckPassed})
	}
	status.Checks = append(status.Checks, Check{Name: "late failure \u202e\x07" + strings.Repeat("é", MaxCheckTextBytes), Workflow: strings.Repeat("w", MaxCheckTextBytes+1), State: CheckFailed})
	got := SummarizeChecks(status)
	if len(got.Checks) != MaxSummaryChecks || got.Omitted != 8 || got.Counts.Passed != MaxSummaryChecks+7 || got.Counts.Failed != 1 {
		t.Fatalf("SummarizeChecks() listed %d, omitted %d, counts %+v", len(got.Checks), got.Omitted, got.Counts)
	}
	first := got.Checks[0]
	wantName := "late failure " + strings.Repeat("é", (MaxCheckTextBytes-len("late failure "))/2)
	if first.State != CheckFailed || first.Name != wantName || first.Workflow != strings.Repeat("w", MaxCheckTextBytes) {
		t.Fatalf("first listed check = %q (%d bytes), workflow %d bytes, state %s", first.Name, len(first.Name), len(first.Workflow), first.State)
	}
}

func TestSummarizeChecksOfNoStatus(t *testing.T) {
	if got := SummarizeChecks(nil); !reflect.DeepEqual(got, ChecksSummary{State: ChecksNone, Required: RequirementUnknown}) {
		t.Fatalf("SummarizeChecks(nil) = %+v", got)
	}
}
