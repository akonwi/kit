package contract

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/akonwi/kit/internal/identifier"
)

// MaxPullRequestChecks bounds the checks one pull request status lists.
const MaxPullRequestChecks = 50

// MaxPullRequestCheckTextBytes bounds a listed check's name and workflow.
const MaxPullRequestCheckTextBytes = 256

// maxPullRequestCheckTotal bounds the counted checks of one pull request. The
// GitHub adapter observes far fewer; the bound only rejects absurd payloads.
const maxPullRequestCheckTotal = 10000

// PullRequestState is a pull request's GitHub lifecycle state.
type PullRequestState string

const (
	PullRequestOpen   PullRequestState = "open"
	PullRequestClosed PullRequestState = "closed"
	PullRequestMerged PullRequestState = "merged"
)

// EnumValues returns the permitted pull request states.
func (PullRequestState) EnumValues() []string {
	return []string{string(PullRequestOpen), string(PullRequestClosed), string(PullRequestMerged)}
}

// PullRequestChecksState is the overall state of a pull request's checks:
// failing when any check failed or was canceled, otherwise pending when any
// is running, otherwise passing when any passed, otherwise none.
type PullRequestChecksState string

const (
	PullRequestChecksPassing PullRequestChecksState = "passing"
	PullRequestChecksFailing PullRequestChecksState = "failing"
	PullRequestChecksPending PullRequestChecksState = "pending"
	PullRequestChecksNone    PullRequestChecksState = "none"
)

// EnumValues returns the permitted overall check states.
func (PullRequestChecksState) EnumValues() []string {
	return []string{string(PullRequestChecksPassing), string(PullRequestChecksFailing), string(PullRequestChecksPending), string(PullRequestChecksNone)}
}

// PullRequestRequirement says whether a repository requirement, such as its
// required checks, is satisfied.
type PullRequestRequirement string

const (
	PullRequestRequirementUnknown     PullRequestRequirement = "unknown"
	PullRequestRequirementPending     PullRequestRequirement = "pending"
	PullRequestRequirementSatisfied   PullRequestRequirement = "satisfied"
	PullRequestRequirementUnsatisfied PullRequestRequirement = "unsatisfied"
)

// EnumValues returns the permitted requirement states.
func (PullRequestRequirement) EnumValues() []string {
	return []string{string(PullRequestRequirementUnknown), string(PullRequestRequirementPending), string(PullRequestRequirementSatisfied), string(PullRequestRequirementUnsatisfied)}
}

// PullRequestCheckState is the state of one check.
type PullRequestCheckState string

const (
	PullRequestCheckPassed   PullRequestCheckState = "passed"
	PullRequestCheckFailed   PullRequestCheckState = "failed"
	PullRequestCheckPending  PullRequestCheckState = "pending"
	PullRequestCheckSkipped  PullRequestCheckState = "skipped"
	PullRequestCheckCanceled PullRequestCheckState = "canceled"
	PullRequestCheckUnknown  PullRequestCheckState = "unknown"
)

// EnumValues returns the permitted check states.
func (PullRequestCheckState) EnumValues() []string {
	return []string{string(PullRequestCheckPassed), string(PullRequestCheckFailed), string(PullRequestCheckPending), string(PullRequestCheckSkipped), string(PullRequestCheckCanceled), string(PullRequestCheckUnknown)}
}

// PullRequestCheck is one listed check.
type PullRequestCheck struct {
	Name     string                `json:"name"`
	Workflow string                `json:"workflow,omitempty"`
	State    PullRequestCheckState `json:"state"`
	Required bool                  `json:"required"`
}

// PullRequestCheckCounts counts every observed check by state.
type PullRequestCheckCounts struct {
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Pending  int `json:"pending"`
	Skipped  int `json:"skipped"`
	Canceled int `json:"canceled"`
	Unknown  int `json:"unknown"`
}

// PullRequestChecks summarizes a pull request's checks. Items lists at most
// MaxPullRequestChecks checks, failing first; Omitted counts the rest, so the
// items and omitted checks together are the counted checks.
type PullRequestChecks struct {
	State    PullRequestChecksState `json:"state"`
	Required PullRequestRequirement `json:"required"`
	Counts   PullRequestCheckCounts `json:"counts"`
	Items    []PullRequestCheck     `json:"items"`
	Omitted  int                    `json:"omitted"`
}

// PullRequestStatus is the observed status of a branch's pull request.
type PullRequestStatus struct {
	Number int               `json:"number"`
	URL    string            `json:"url"`
	State  PullRequestState  `json:"state"`
	Draft  bool              `json:"draft"`
	Checks PullRequestChecks `json:"checks"`
}

// SessionPullRequestStatus binds the observed pull request of a session
// workspace's named branch to the session and cwd. Branch is empty outside a
// named branch; PullRequest is nil without a branch, while the lookup is
// pending, or when the branch has no pull request or GitHub is unavailable.
type SessionPullRequestStatus struct {
	SessionID   string             `json:"sessionId"`
	CWD         string             `json:"cwd"`
	Branch      string             `json:"branch,omitempty"`
	PullRequest *PullRequestStatus `json:"pullRequest,omitempty"`
}

// Validate checks a pull request status received across a transport boundary.
func (value SessionPullRequestStatus) Validate() error {
	if !identifier.Valid(value.SessionID, "session_") {
		return fmt.Errorf("pull request status session id is invalid")
	}
	if !filepath.IsAbs(value.CWD) || !validPathText(value.CWD) {
		return fmt.Errorf("pull request status cwd is not a safe bounded absolute path")
	}
	if value.Branch != "" && !validRendererText(value.Branch, 4096) {
		return fmt.Errorf("pull request status branch is invalid")
	}
	pr := value.PullRequest
	if pr == nil {
		return nil
	}
	if value.Branch == "" {
		return fmt.Errorf("pull request status has a pull request without a branch")
	}
	if pr.Number <= 0 || int64(pr.Number) > 9007199254740991 || !validPullRequestURL(pr.URL) {
		return fmt.Errorf("pull request identity is invalid")
	}
	if !validEnum(pr.State) {
		return fmt.Errorf("pull request state is invalid")
	}
	return pr.Checks.Validate()
}

// Validate checks the bounds and consistency of a check summary.
func (checks PullRequestChecks) Validate() error {
	if !validEnum(checks.State) || !validEnum(checks.Required) {
		return fmt.Errorf("pull request checks state is invalid")
	}
	counts := checks.Counts
	for _, count := range []int{counts.Passed, counts.Failed, counts.Pending, counts.Skipped, counts.Canceled, counts.Unknown} {
		if count < 0 || count > maxPullRequestCheckTotal {
			return fmt.Errorf("pull request check count is out of range")
		}
	}
	total := counts.Passed + counts.Failed + counts.Pending + counts.Skipped + counts.Canceled + counts.Unknown
	if total > maxPullRequestCheckTotal || checks.Items == nil || len(checks.Items) > MaxPullRequestChecks || checks.Omitted < 0 || len(checks.Items)+checks.Omitted != total {
		return fmt.Errorf("pull request checks are inconsistent")
	}
	if checks.Omitted > 0 && len(checks.Items) != MaxPullRequestChecks {
		return fmt.Errorf("pull request checks omit checks below the list bound")
	}
	var want PullRequestChecksState
	switch {
	case counts.Failed > 0 || counts.Canceled > 0:
		want = PullRequestChecksFailing
	case counts.Pending > 0:
		want = PullRequestChecksPending
	case counts.Passed > 0:
		want = PullRequestChecksPassing
	default:
		want = PullRequestChecksNone
	}
	if checks.State != want {
		return fmt.Errorf("pull request checks state disagrees with its counts")
	}
	listed := PullRequestCheckCounts{}
	for _, check := range checks.Items {
		if !validRendererText(check.Name, MaxPullRequestCheckTextBytes) || check.Workflow != "" && !validRendererText(check.Workflow, MaxPullRequestCheckTextBytes) || !validEnum(check.State) {
			return fmt.Errorf("pull request check is invalid")
		}
		switch check.State {
		case PullRequestCheckPassed:
			listed.Passed++
		case PullRequestCheckFailed:
			listed.Failed++
		case PullRequestCheckPending:
			listed.Pending++
		case PullRequestCheckSkipped:
			listed.Skipped++
		case PullRequestCheckCanceled:
			listed.Canceled++
		default:
			listed.Unknown++
		}
	}
	if listed.Passed > counts.Passed || listed.Failed > counts.Failed || listed.Pending > counts.Pending || listed.Skipped > counts.Skipped || listed.Canceled > counts.Canceled || listed.Unknown > counts.Unknown {
		return fmt.Errorf("pull request checks list more checks than they count")
	}
	return nil
}

func validEnum[T interface {
	~string
	EnumValues() []string
}](value T) bool {
	for _, allowed := range value.EnumValues() {
		if string(value) == allowed {
			return true
		}
	}
	return false
}

// validPullRequestURL accepts bounded absolute HTTP(S) destinations without
// credentials, whitespace, backslashes, or control characters.
func validPullRequestURL(value string) bool {
	if !validRendererText(value, 4096) || strings.ContainsAny(value, "\\") {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) {
			return false
		}
	}
	target, err := url.Parse(value)
	return err == nil && (target.Scheme == "https" || target.Scheme == "http") && target.Hostname() != "" && target.User == nil && target.Opaque == ""
}
