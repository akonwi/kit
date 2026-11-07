package pullrequest

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ChecksState is the overall state of a pull request's checks.
type ChecksState string

const (
	// ChecksNone means no check has run or will run: there are no checks, or
	// every check was skipped or reported an unknown state.
	ChecksNone ChecksState = "none"
	// ChecksPending means no check failed and at least one is still running.
	ChecksPending ChecksState = "pending"
	// ChecksPassing means no check failed or is running and at least one passed.
	ChecksPassing ChecksState = "passing"
	// ChecksFailing means at least one check failed or was canceled.
	ChecksFailing ChecksState = "failing"
)

// MaxSummaryChecks bounds the checks a summary lists. Counts still cover
// every observed check.
const MaxSummaryChecks = 50

// MaxCheckTextBytes bounds a listed check's name and workflow in UTF-8 bytes.
const MaxCheckTextBytes = 256

// CheckCounts counts checks by state.
type CheckCounts struct {
	Passed   int
	Failed   int
	Pending  int
	Skipped  int
	Canceled int
	Unknown  int
}

// Total is the number of counted checks.
func (c CheckCounts) Total() int {
	return c.Passed + c.Failed + c.Pending + c.Skipped + c.Canceled + c.Unknown
}

// ChecksSummary is a bounded, renderer-neutral account of a pull request's
// checks. Checks lists at most MaxSummaryChecks checks, most urgent first;
// Omitted counts the rest.
type ChecksSummary struct {
	State    ChecksState
	Required RequirementState
	Counts   CheckCounts
	Checks   []Check
	Omitted  int
}

// SummarizeChecks returns the bounded summary of status's checks. A nil
// status has no checks.
func SummarizeChecks(status *Status) ChecksSummary {
	result := ChecksSummary{State: ChecksNone, Required: RequirementUnknown}
	if status == nil {
		return result
	}
	if status.RequiredChecks != "" {
		result.Required = status.RequiredChecks
	}
	checks := make([]Check, 0, len(status.Checks))
	for _, check := range status.Checks {
		check.Name = boundedText(check.Name)
		check.Workflow = boundedText(check.Workflow)
		if check.Name == "" {
			check.Name = "Unnamed check"
		}
		switch check.State {
		case CheckPassed:
			result.Counts.Passed++
		case CheckFailed:
			result.Counts.Failed++
		case CheckPending:
			result.Counts.Pending++
		case CheckSkipped:
			result.Counts.Skipped++
		case CheckCanceled:
			result.Counts.Canceled++
		default:
			check.State = CheckUnknown
			result.Counts.Unknown++
		}
		checks = append(checks, check)
	}
	switch {
	case result.Counts.Failed > 0 || result.Counts.Canceled > 0:
		result.State = ChecksFailing
	case result.Counts.Pending > 0:
		result.State = ChecksPending
	case result.Counts.Passed > 0:
		result.State = ChecksPassing
	}
	// Keep the adapter's stable order within a state so a capped list keeps
	// the checks that need attention.
	sort.SliceStable(checks, func(i, j int) bool { return urgency(checks[i].State) < urgency(checks[j].State) })
	if len(checks) > MaxSummaryChecks {
		result.Omitted = len(checks) - MaxSummaryChecks
		checks = checks[:MaxSummaryChecks]
	}
	if len(checks) > 0 {
		result.Checks = checks
	}
	return result
}

func urgency(state CheckState) int {
	switch state {
	case CheckFailed:
		return 0
	case CheckCanceled:
		return 1
	case CheckPending:
		return 2
	case CheckUnknown:
		return 3
	case CheckPassed:
		return 4
	default:
		return 5
	}
}

// boundedText removes control and format characters and truncates value to
// MaxCheckTextBytes on a rune boundary.
func boundedText(value string) string {
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "\uFFFD")
	}
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if builder.Len()+utf8.RuneLen(r) > MaxCheckTextBytes {
			break
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}
