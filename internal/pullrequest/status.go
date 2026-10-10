// Package pullrequest defines renderer-neutral pull-request observation state.
package pullrequest

// CheckState is the normalized state of one CI check.
type CheckState string

const (
	CheckPending  CheckState = "pending"
	CheckPassed   CheckState = "passed"
	CheckFailed   CheckState = "failed"
	CheckSkipped  CheckState = "skipped"
	CheckCanceled CheckState = "canceled"
	CheckUnknown  CheckState = "unknown"
)

// RequirementState describes whether a repository requirement is satisfied.
type RequirementState string

const (
	RequirementUnknown     RequirementState = "unknown"
	RequirementPending     RequirementState = "pending"
	RequirementSatisfied   RequirementState = "satisfied"
	RequirementUnsatisfied RequirementState = "unsatisfied"
)

// Readiness is Kit's conservative projection of GitHub merge readiness.
type Readiness string

const (
	ReadinessUnknown Readiness = "unknown"
	ReadinessBlocked Readiness = "blocked"
	ReadinessReady   Readiness = "ready"
)

// Check is one canonical CI check. Name and Workflow originate outside Kit.
type Check struct {
	Name     string     `json:"name"`
	Workflow string     `json:"workflow,omitempty"`
	State    CheckState `json:"state"`
	Required bool       `json:"required,omitempty"`
}

// Review is one reviewer's latest canonical review state.
type Review struct {
	Author string `json:"author"`
	State  string `json:"state"`
}

// Status is the canonical, renderer-neutral state of one open pull request.
// Slices are sorted by the GitHub adapter so structural equality is stable.
type Status struct {
	Number            int              `json:"number"`
	URL               string           `json:"url"`
	HeadRefName       string           `json:"headRefName"`
	HeadOID           string           `json:"headOid,omitempty"`
	State             string           `json:"state"`
	Draft             bool             `json:"draft"`
	Mergeable         string           `json:"mergeable,omitempty"`
	MergeState        string           `json:"mergeState,omitempty"`
	ReviewDecision    string           `json:"reviewDecision,omitempty"`
	RequiredChecks    RequirementState `json:"requiredChecks"`
	RequiredApprovals RequirementState `json:"requiredApprovals"`
	Readiness         Readiness        `json:"readiness"`
	Checks            []Check          `json:"checks,omitempty"`
	Reviews           []Review         `json:"reviews,omitempty"`
}

// Clone returns a detached copy safe for another owner to retain.
func Clone(value *Status) *Status {
	if value == nil {
		return nil
	}
	result := *value
	result.Checks = append([]Check(nil), value.Checks...)
	result.Reviews = append([]Review(nil), value.Reviews...)
	return &result
}
