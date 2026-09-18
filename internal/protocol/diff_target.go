package protocol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	DiffTargetWorkingTree = "working_tree"
	DiffTargetCommit      = "commit"
	DiffTargetBranch      = "branch"

	MaxDiffTargets           = 42 // working tree, current branch, 20 local choices, and 20 commits.
	MaxDiffTargetDiagnostics = 20
	MaxDiffTargetReference   = 2048
)

// DiffEndpoint is one pinned side of a diff target. Empty trees have no OID.
type DiffEndpoint struct {
	Kind string `json:"kind"`
	OID  string `json:"oid,omitempty"`
}

// DiffTargetMetadata is bounded, non-authoritative presentation data.
type DiffTargetMetadata struct {
	Label       string `json:"label"`
	RefName     string `json:"refName,omitempty"`
	BaseRefName string `json:"baseRefName,omitempty"`
	Subject     string `json:"subject,omitempty"`
	Abbreviated string `json:"abbreviatedOid,omitempty"`
	CommittedAt int64  `json:"committedAt,omitempty"`
}

// DiffTargetEntry is a server-resolved selectable target. Reference is the only
// authority accepted by ObserveDiff; endpoints and metadata are informational.
type DiffTargetEntry struct {
	Reference       string             `json:"reference"`
	TargetID        string             `json:"targetId"`
	Kind            string             `json:"kind"`
	Base            DiffEndpoint       `json:"base"`
	Head            DiffEndpoint       `json:"head"`
	Metadata        DiffTargetMetadata `json:"metadata"`
	AnnotationCount *int               `json:"annotationCount,omitempty"`
}

// DiffTargetDiagnostic reports one bounded catalog omission without exposing
// unsafe ref input or command output.
type DiffTargetDiagnostic struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// DiffTargetCatalog is one repository-state snapshot of selectable targets.
type DiffTargetCatalog struct {
	SessionID   string                 `json:"sessionId"`
	WorkspaceID string                 `json:"workspaceId"`
	Targets     []DiffTargetEntry      `json:"targets"`
	Diagnostics []DiffTargetDiagnostic `json:"diagnostics"`
}

type ListDiffTargetsInput struct {
	WorkspaceID string `json:"workspaceId"`
}

// ObserveDiffInput selects a server-issued target reference.
type ObserveDiffInput struct {
	WorkspaceID      string `json:"workspaceId"`
	TargetReference  string `json:"targetReference"`
	ExpectedTargetID string `json:"expectedTargetId"`
	PageSize         int    `json:"pageSize,omitempty"`
	Cursor           string `json:"cursor,omitempty"`
}

// DiffPage is the generalized observation page. WorkingTreePage remains an
// alias so existing consumers can migrate independently.
type DiffPage = WorkingTreePage

func validTargetReference(v string) bool {
	if len(v) <= len("difftargetref_")+2 || len(v) > MaxDiffTargetReference || !strings.HasPrefix(v, "difftargetref_") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(v, "difftargetref_"), ".")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		b, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil || len(b) == 0 || base64.RawURLEncoding.EncodeToString(b) != part {
			return false
		}
	}
	return true
}

func (in ListDiffTargetsInput) Validate() error {
	if !validWorkspaceToken(in.WorkspaceID, "workspace_") {
		return fmt.Errorf("diff target catalog request is invalid")
	}
	return nil
}

func (in ObserveDiffInput) Validate() error {
	if !validWorkspaceToken(in.WorkspaceID, "workspace_") || !validTargetReference(in.TargetReference) || !validDiffToken(in.ExpectedTargetID, "difftarget_") || in.PageSize < 0 || in.PageSize > MaxDiffFilePageSize || len(in.Cursor) > MaxDiffCursorBytes {
		return fmt.Errorf("diff observation request is invalid")
	}
	return nil
}

func (e DiffEndpoint) validate() error {
	switch e.Kind {
	case "empty_tree":
		if e.OID != "" {
			return fmt.Errorf("empty diff endpoint has an object id")
		}
	case "commit":
		if !validHexOID(e.OID) {
			return fmt.Errorf("commit diff endpoint is invalid")
		}
	default:
		return fmt.Errorf("diff endpoint kind is invalid")
	}
	return nil
}

func (e DiffTargetEntry) Validate() error {
	if !validTargetReference(e.Reference) || !validDiffToken(e.TargetID, "difftarget_") || e.Base.validate() != nil || e.Head.validate() != nil || !validRendererText(e.Metadata.Label, 160) || !validOptionalRendererText(e.Metadata.RefName, 256) || !validOptionalRendererText(e.Metadata.BaseRefName, 256) || !validOptionalRendererText(e.Metadata.Subject, 256) || len(e.Metadata.Abbreviated) > 16 || e.Metadata.Abbreviated != "" && !validHexPrefix(e.Metadata.Abbreviated) || e.Metadata.CommittedAt < 0 {
		return fmt.Errorf("diff target entry is invalid")
	}
	switch e.Kind {
	case DiffTargetWorkingTree:
		if e.Base.Kind != "empty_tree" && e.Base.Kind != "commit" || e.Head != e.Base || e.Metadata.RefName != "" || e.Metadata.BaseRefName != "" {
			return fmt.Errorf("working-tree target is invalid")
		}
	case DiffTargetCommit:
		if e.Head.Kind != "commit" || e.Metadata.Subject == "" || e.Metadata.Abbreviated == "" {
			return fmt.Errorf("commit target is invalid")
		}
	case DiffTargetBranch:
		if e.Base.Kind != "commit" || e.Head.Kind != "commit" || e.Metadata.RefName == "" || e.Metadata.BaseRefName == "" {
			return fmt.Errorf("branch target is invalid")
		}
	default:
		return fmt.Errorf("diff target kind is invalid")
	}
	if e.AnnotationCount != nil && *e.AnnotationCount < 0 {
		return fmt.Errorf("diff target annotation count is invalid")
	}
	return nil
}

func validOptionalRendererText(v string, n int) bool { return v == "" || validRendererText(v, n) }
func validHexPrefix(v string) bool {
	if len(v) < 7 {
		return false
	}
	for _, r := range v {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func (c DiffTargetCatalog) Validate() error {
	if c.SessionID == "" || !validWorkspaceToken(c.WorkspaceID, "workspace_") || c.Targets == nil || c.Diagnostics == nil || len(c.Targets) == 0 || len(c.Targets) > MaxDiffTargets || len(c.Diagnostics) > MaxDiffTargetDiagnostics {
		return fmt.Errorf("diff target catalog is invalid")
	}
	ids := map[string]bool{}
	references := map[string]bool{}
	for i, target := range c.Targets {
		if target.Validate() != nil || ids[target.TargetID] || references[target.Reference] || i == 0 && target.Kind != DiffTargetWorkingTree {
			return fmt.Errorf("diff target catalog entries are invalid")
		}
		ids[target.TargetID] = true
		references[target.Reference] = true
	}
	for _, diagnostic := range c.Diagnostics {
		if !map[string]bool{"invalid_ref": true, "missing_object": true, "unsupported_ref": true, "branch_limit": true, "commit_limit": true}[diagnostic.Reason] || diagnostic.Count < 1 {
			return fmt.Errorf("diff target diagnostic is invalid")
		}
	}
	b, _ := json.Marshal(c)
	if len(b) > MaxDiffResponseBytes {
		return fmt.Errorf("diff target catalog is too large")
	}
	return nil
}
