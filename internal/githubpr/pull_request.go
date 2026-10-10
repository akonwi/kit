// Package githubpr provides optional, bounded GitHub metadata through the user's gh CLI.
package githubpr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/pullrequest"
)

const lookupTimeout = 5 * time.Second
const maxOutputBytes = 64 << 10
const maxStatusItems = 48
const maxStatusTextBytes = 256

// PullRequest is the canonical metadata observed for the current named branch.
type PullRequest = pullrequest.Status

// ValidURL accepts bounded absolute HTTP(S) destinations without credentials or controls.
func ValidURL(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\\r\n\t ") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) {
			return false
		}
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Hostname() != "" && parsed.User == nil && parsed.Opaque == ""
}

func validStatusText(value string) bool {
	if len(value) > maxStatusTextBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func validOID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

type viewCheck struct {
	Type       string `json:"__typename"`
	Name       string `json:"name"`
	Context    string `json:"context"`
	Workflow   string `json:"workflowName"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

type viewReview struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State string `json:"state"`
}

type viewPullRequest struct {
	Number         int          `json:"number"`
	URL            string       `json:"url"`
	HeadRefName    string       `json:"headRefName"`
	HeadOID        string       `json:"headRefOid"`
	State          string       `json:"state"`
	Draft          bool         `json:"isDraft"`
	Mergeable      string       `json:"mergeable"`
	MergeState     string       `json:"mergeStateStatus"`
	ReviewDecision string       `json:"reviewDecision"`
	Checks         []viewCheck  `json:"statusCheckRollup"`
	LatestReviews  []viewReview `json:"latestReviews"`
}

type requiredCheck struct {
	Bucket   string `json:"bucket"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Workflow string `json:"workflow"`
}

func parse(raw []byte, branch string) *PullRequest {
	return parseObservation(raw, nil, branch)
}

func parseObservation(raw, requiredRaw []byte, branch string) *PullRequest {
	if !utf8.Valid(raw) || !utf8.Valid(requiredRaw) {
		return nil
	}
	var value viewPullRequest
	if json.Unmarshal(raw, &value) != nil || value.Number <= 0 || int64(value.Number) > 9007199254740991 || !ValidURL(value.URL) || value.HeadRefName != branch || !validStatusText(branch) {
		return nil
	}
	if value.HeadOID != "" && !validOID(value.HeadOID) {
		return nil
	}
	if value.State == "" {
		value.State = "OPEN"
	}
	state, ok := canonicalEnum(value.State, "OPEN", "CLOSED", "MERGED")
	if !ok {
		return nil
	}
	mergeable, ok := canonicalEnum(value.Mergeable, "", "MERGEABLE", "CONFLICTING", "UNKNOWN")
	if !ok {
		return nil
	}
	mergeState, ok := canonicalEnum(value.MergeState, "", "BEHIND", "BLOCKED", "CLEAN", "DIRTY", "HAS_HOOKS", "UNKNOWN", "UNSTABLE")
	if !ok {
		return nil
	}
	reviewDecision, ok := canonicalEnum(value.ReviewDecision, "", "APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED")
	if !ok || len(value.Checks) > maxStatusItems || len(value.LatestReviews) > maxStatusItems {
		return nil
	}
	result := &pullrequest.Status{
		Number: value.Number, URL: value.URL, HeadRefName: branch, HeadOID: value.HeadOID,
		State: state, Draft: value.Draft, Mergeable: mergeable, MergeState: mergeState,
		ReviewDecision: reviewDecision, RequiredChecks: pullrequest.RequirementUnknown,
		RequiredApprovals: approvalRequirement(reviewDecision),
	}
	for _, rawCheck := range value.Checks {
		name := rawCheck.Name
		if name == "" {
			name = rawCheck.Context
		}
		if name == "" || !validStatusText(name) || !validStatusText(rawCheck.Workflow) {
			return nil
		}
		result.Checks = append(result.Checks, pullrequest.Check{Name: name, Workflow: rawCheck.Workflow, State: checkState(rawCheck)})
	}
	for _, rawReview := range value.LatestReviews {
		if rawReview.Author.Login == "" {
			continue
		}
		if !validStatusText(rawReview.Author.Login) {
			return nil
		}
		reviewState, ok := canonicalEnum(rawReview.State, "APPROVED", "CHANGES_REQUESTED", "COMMENTED", "DISMISSED", "PENDING")
		if !ok {
			return nil
		}
		result.Reviews = append(result.Reviews, pullrequest.Review{Author: rawReview.Author.Login, State: reviewState})
	}
	if requiredRaw != nil {
		var required []requiredCheck
		if json.Unmarshal(requiredRaw, &required) == nil && len(required) <= maxStatusItems {
			applyRequiredChecks(result, required)
		}
	}
	sort.Slice(result.Checks, func(i, j int) bool {
		left, right := result.Checks[i], result.Checks[j]
		if left.Required != right.Required {
			return left.Required
		}
		if left.Workflow != right.Workflow {
			return left.Workflow < right.Workflow
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.State < right.State
	})
	sort.Slice(result.Reviews, func(i, j int) bool {
		if result.Reviews[i].Author != result.Reviews[j].Author {
			return result.Reviews[i].Author < result.Reviews[j].Author
		}
		return result.Reviews[i].State < result.Reviews[j].State
	})
	result.Readiness = readiness(result)
	return result
}

func canonicalEnum(value string, allowed ...string) (string, bool) {
	value = strings.ToUpper(value)
	for _, candidate := range allowed {
		if value == candidate {
			return value, true
		}
	}
	return "", false
}

func checkState(value viewCheck) pullrequest.CheckState {
	state := strings.ToUpper(value.State)
	status := strings.ToUpper(value.Status)
	conclusion := strings.ToUpper(value.Conclusion)
	if status != "" && status != "COMPLETED" {
		return pullrequest.CheckPending
	}
	switch conclusion {
	case "SUCCESS":
		return pullrequest.CheckPassed
	case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE", "STALE":
		return pullrequest.CheckFailed
	case "SKIPPED", "NEUTRAL":
		return pullrequest.CheckSkipped
	case "CANCELLED":
		return pullrequest.CheckCanceled
	}
	switch state {
	case "SUCCESS":
		return pullrequest.CheckPassed
	case "FAILURE", "ERROR":
		return pullrequest.CheckFailed
	case "PENDING", "EXPECTED":
		return pullrequest.CheckPending
	}
	if status == "QUEUED" || status == "IN_PROGRESS" || status == "PENDING" || conclusion == "" && status != "COMPLETED" {
		return pullrequest.CheckPending
	}
	return pullrequest.CheckUnknown
}

func requiredCheckState(bucket string) pullrequest.CheckState {
	switch strings.ToLower(bucket) {
	case "pass":
		return pullrequest.CheckPassed
	case "fail":
		return pullrequest.CheckFailed
	case "pending":
		return pullrequest.CheckPending
	case "skipping":
		return pullrequest.CheckSkipped
	case "cancel":
		return pullrequest.CheckCanceled
	default:
		return pullrequest.CheckUnknown
	}
}

func applyRequiredChecks(result *pullrequest.Status, required []requiredCheck) {
	result.RequiredChecks = pullrequest.RequirementSatisfied
	for _, raw := range required {
		if raw.Name == "" || !validStatusText(raw.Name) || !validStatusText(raw.Workflow) {
			result.RequiredChecks = pullrequest.RequirementUnknown
			return
		}
		state := requiredCheckState(raw.Bucket)
		matched := false
		for i := range result.Checks {
			if result.Checks[i].Name == raw.Name && result.Checks[i].Workflow == raw.Workflow {
				result.Checks[i].Required = true
				result.Checks[i].State = state
				matched = true
			}
		}
		if !matched {
			result.Checks = append(result.Checks, pullrequest.Check{Name: raw.Name, Workflow: raw.Workflow, State: state, Required: true})
		}
		switch state {
		case pullrequest.CheckFailed, pullrequest.CheckCanceled:
			result.RequiredChecks = pullrequest.RequirementUnsatisfied
		case pullrequest.CheckPending:
			if result.RequiredChecks != pullrequest.RequirementUnsatisfied {
				result.RequiredChecks = pullrequest.RequirementPending
			}
		case pullrequest.CheckUnknown:
			if result.RequiredChecks == pullrequest.RequirementSatisfied {
				result.RequiredChecks = pullrequest.RequirementUnknown
			}
		}
	}
}

func approvalRequirement(decision string) pullrequest.RequirementState {
	switch decision {
	case "APPROVED":
		return pullrequest.RequirementSatisfied
	case "CHANGES_REQUESTED":
		return pullrequest.RequirementUnsatisfied
	case "REVIEW_REQUIRED":
		return pullrequest.RequirementPending
	default:
		return pullrequest.RequirementUnknown
	}
}

func readiness(value *pullrequest.Status) pullrequest.Readiness {
	if value.State != "OPEN" || value.Draft || value.Mergeable == "CONFLICTING" || value.MergeState == "DIRTY" || value.MergeState == "BLOCKED" || value.MergeState == "BEHIND" {
		return pullrequest.ReadinessBlocked
	}
	if value.RequiredChecks == pullrequest.RequirementUnsatisfied || value.RequiredChecks == pullrequest.RequirementPending || value.RequiredApprovals == pullrequest.RequirementUnsatisfied || value.RequiredApprovals == pullrequest.RequirementPending {
		return pullrequest.ReadinessBlocked
	}
	if value.Mergeable == "MERGEABLE" && value.MergeState == "CLEAN" {
		return pullrequest.ReadinessReady
	}
	return pullrequest.ReadinessUnknown
}

// Lookup returns nil silently when gh, authentication, a matching PR, or valid metadata
// is unavailable. The explicit branch prevents a concurrent checkout from retargeting it.
func Lookup(ctx context.Context, cwd, branch string) *PullRequest {
	if branch == "" || len(branch) > 4096 || !utf8.ValidString(branch) || strings.ContainsRune(branch, 0) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	fields := "number,url,headRefName,headRefOid,state,isDraft,mergeable,mergeStateStatus,reviewDecision,statusCheckRollup,latestReviews"
	output, code, ok := runGH(ctx, cwd, "pr", "view", "--json", fields, "--", branch)
	if !ok || code != 0 {
		return nil
	}
	var identity struct {
		Number int `json:"number"`
	}
	if json.Unmarshal(output, &identity) != nil || identity.Number <= 0 {
		return nil
	}
	required, requiredCode, requiredOK := runGH(ctx, cwd, "pr", "checks", strconv.Itoa(identity.Number), "--required", "--json", "bucket,name,state,workflow")
	if !requiredOK || requiredCode != 0 && requiredCode != 1 && requiredCode != 8 {
		required = nil
	}
	return parseObservation(output, required, branch)
}

func runGH(ctx context.Context, cwd string, arguments ...string) ([]byte, int, bool) {
	command := exec.CommandContext(ctx, "gh", arguments...)
	command.Dir = cwd
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") || key == "GH_REPO" || key == "GH_PROMPT_DISABLED" || key == "GH_PAGER" || key == "PAGER" {
			continue
		}
		command.Env = append(command.Env, entry)
	}
	command.Env = append(command.Env, "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "PAGER=cat", "GIT_TERMINAL_PROMPT=0")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 250 * time.Millisecond
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	var output cappedBuffer
	command.Stdout = &output
	command.Stderr = io.Discard
	err := command.Run()
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if ctx.Err() != nil || output.overflow {
		return nil, 0, false
	}
	if err == nil {
		return output.buffer.Bytes(), 0, true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return output.buffer.Bytes(), exit.ExitCode(), true
	}
	return nil, 0, false
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	n := len(data)
	remaining := maxOutputBytes - b.buffer.Len()
	if remaining > 0 {
		_, _ = b.buffer.Write(data[:min(n, remaining)])
	}
	if n > remaining {
		b.overflow = true
	}
	return n, nil
}
