package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
)

const (
	MaxAnnotationSummaryTextBytes   = 256
	MaxAnnotationBodyBytes          = 16 << 10
	MaxAnnotationPreviewBytes       = 16 << 10
	MaxAnnotationRangeLines         = 200
	MaxLiveAnnotationsPerSession    = 128
	MaxAnnotationsPerPrompt         = 64
	MaxPromptAnnotationBytes        = 256 << 10
	DefaultAnnotationPageSize       = 50
	MaxAnnotationPageSize           = 100
	MaxAnnotationCursorBytes        = 256
	MaxAnnotationStaleReasonBytes   = 256
	MaxSubmittedAnnotationJSONBytes = 32 << 10
)

// AnnotationAnchorKind identifies one explicitly validated evidence anchor.
type AnnotationAnchorKind string

const (
	AnnotationAnchorWorkspaceFile   AnnotationAnchorKind = "workspace_file"
	AnnotationAnchorWorkingTreeDiff AnnotationAnchorKind = "working_tree_diff"
)

// WorkspaceFileAnnotationAnchor pins a one-based inclusive line range to an
// observed session workspace file revision.
type WorkspaceFileAnnotationAnchor struct {
	WorkspaceID  string `json:"workspaceId"`
	Path         string `json:"path"`
	FileRevision string `json:"fileRevision"`
	StartLine    int    `json:"startLine"`
	EndLine      int    `json:"endLine"`
}

// WorkingTreeDiffAnnotationAnchor pins a source-side line range to one retained
// working-tree diff observation and changed-file revision.
type WorkingTreeDiffAnnotationAnchor struct {
	TargetID       string `json:"targetId"`
	TargetRevision string `json:"targetRevision"`
	Path           string `json:"path"`
	FileRevision   string `json:"fileRevision"`
	Side           string `json:"side"`
	StartLine      int    `json:"startLine"`
	EndLine        int    `json:"endLine"`
}

// AnnotationAnchor is a tagged union of supported evidence anchors.
type AnnotationAnchor struct {
	Kind            AnnotationAnchorKind             `json:"kind"`
	WorkspaceFile   *WorkspaceFileAnnotationAnchor   `json:"workspaceFile,omitempty"`
	WorkingTreeDiff *WorkingTreeDiffAnnotationAnchor `json:"workingTreeDiff,omitempty"`
}

// Validate checks the anchor tag, exact variant, identity, and range bounds.
func (a AnnotationAnchor) Validate() error {
	switch a.Kind {
	case AnnotationAnchorWorkspaceFile:
		if a.WorkspaceFile == nil || a.WorkingTreeDiff != nil {
			return fmt.Errorf("annotation anchor variant is invalid")
		}
		anchor := a.WorkspaceFile
		if !validWorkspaceToken(anchor.WorkspaceID, "workspace_") || !validWorkspaceToken(anchor.FileRevision, "file_") || ValidateWorkspacePath(anchor.Path, false) != nil {
			return fmt.Errorf("workspace file annotation identity is invalid")
		}
		if !validAnnotationRange(anchor.StartLine, anchor.EndLine) {
			return fmt.Errorf("workspace file annotation range is invalid")
		}
	case AnnotationAnchorWorkingTreeDiff:
		if a.WorkspaceFile != nil || a.WorkingTreeDiff == nil {
			return fmt.Errorf("annotation anchor variant is invalid")
		}
		anchor := a.WorkingTreeDiff
		if !validDiffToken(anchor.TargetID, "difftarget_") || !validDiffToken(anchor.TargetRevision, "diffrev_") || !validDiffToken(anchor.FileRevision, "diff_file_") || ValidateWorkspacePath(anchor.Path, false) != nil || anchor.Side != "old" && anchor.Side != "new" {
			return fmt.Errorf("working-tree diff annotation identity is invalid")
		}
		if !validAnnotationRange(anchor.StartLine, anchor.EndLine) {
			return fmt.Errorf("working-tree diff annotation range is invalid")
		}
	default:
		return fmt.Errorf("annotation anchor kind is invalid")
	}
	return nil
}

func validAnnotationRange(start, end int) bool {
	return start > 0 && end >= start && end-start+1 <= MaxAnnotationRangeLines
}

func annotationAnchorRange(anchor AnnotationAnchor) (int, int) {
	if anchor.WorkspaceFile != nil {
		return anchor.WorkspaceFile.StartLine, anchor.WorkspaceFile.EndLine
	}
	if anchor.WorkingTreeDiff != nil {
		return anchor.WorkingTreeDiff.StartLine, anchor.WorkingTreeDiff.EndLine
	}
	return 0, 0
}

// AnnotationPreview is frozen server-derived evidence for one annotation.
type AnnotationPreview struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Validate checks preview range, text safety, and bounds.
func (p AnnotationPreview) Validate(anchor AnnotationAnchor) error {
	if err := anchor.Validate(); err != nil {
		return err
	}
	start, end := annotationAnchorRange(anchor)
	if p.StartLine != start || p.EndLine != end || len(p.Text) > MaxAnnotationPreviewBytes || !validAnnotationText(p.Text, true) {
		return fmt.Errorf("annotation preview is invalid")
	}
	return nil
}

// AnnotationStaleReason explains why a live annotation cannot be submitted.
type AnnotationStaleReason string

const (
	AnnotationStaleWorkspace   AnnotationStaleReason = "workspace_changed"
	AnnotationStaleTarget      AnnotationStaleReason = "target_changed"
	AnnotationStaleFile        AnnotationStaleReason = "file_changed"
	AnnotationStaleUnavailable AnnotationStaleReason = "resource_unavailable"
)

// Annotation is one server-owned input for a message being drafted.
type Annotation struct {
	ID          uint64                `json:"id"`
	SessionID   string                `json:"sessionId"`
	Anchor      AnnotationAnchor      `json:"anchor"`
	Body        string                `json:"body"`
	Preview     AnnotationPreview     `json:"preview"`
	Stale       bool                  `json:"stale,omitempty"`
	StaleReason AnnotationStaleReason `json:"staleReason,omitempty"`
}

// Validate checks one renderer-safe annotation projection.
func (a Annotation) Validate() error {
	if a.ID == 0 || !identifier.Valid(a.SessionID, "session_") || a.Anchor.Validate() != nil || a.Preview.Validate(a.Anchor) != nil || !validAnnotationText(a.Body, false) || len(a.Body) > MaxAnnotationBodyBytes {
		return fmt.Errorf("annotation is invalid")
	}
	if a.Stale != (a.StaleReason != "") {
		return fmt.Errorf("annotation stale state is invalid")
	}
	if a.StaleReason != "" && a.StaleReason != AnnotationStaleWorkspace && a.StaleReason != AnnotationStaleTarget && a.StaleReason != AnnotationStaleFile && a.StaleReason != AnnotationStaleUnavailable {
		return fmt.Errorf("annotation stale reason is invalid")
	}
	return nil
}

// CreateAnnotationInput requests a server-derived annotation at an exact anchor.
type CreateAnnotationInput struct {
	Anchor AnnotationAnchor `json:"anchor"`
	Body   string           `json:"body"`
}

// Validate checks annotation creation input.
func (in CreateAnnotationInput) Validate() error {
	if err := in.Anchor.Validate(); err != nil {
		return err
	}
	if !validAnnotationText(in.Body, false) || len(in.Body) > MaxAnnotationBodyBytes {
		return fmt.Errorf("annotation body is invalid")
	}
	return nil
}

// UpdateAnnotationInput replaces the body of one live annotation.
type UpdateAnnotationInput struct {
	AnnotationID uint64 `json:"annotationId"`
	Body         string `json:"body"`
}

// Validate checks annotation update input.
func (in UpdateAnnotationInput) Validate() error {
	if in.AnnotationID == 0 || !validAnnotationText(in.Body, false) || len(in.Body) > MaxAnnotationBodyBytes {
		return fmt.Errorf("annotation update is invalid")
	}
	return nil
}

// DeleteAnnotationInput removes one live annotation.
type DeleteAnnotationInput struct {
	AnnotationID uint64 `json:"annotationId"`
}

// Validate checks annotation deletion input.
func (in DeleteAnnotationInput) Validate() error {
	if in.AnnotationID == 0 {
		return fmt.Errorf("annotation id is invalid")
	}
	return nil
}

// ListAnnotationsInput requests one bounded page ordered by annotation ID.
type ListAnnotationsInput struct {
	Cursor   string `json:"cursor,omitempty"`
	PageSize int    `json:"pageSize,omitempty"`
}

// Validate checks annotation list bounds.
func (in ListAnnotationsInput) Validate() error {
	if in.PageSize < 0 || in.PageSize > MaxAnnotationPageSize || len(in.Cursor) > MaxAnnotationCursorBytes || in.Cursor != "" && !validRendererText(in.Cursor, MaxAnnotationCursorBytes) {
		return fmt.Errorf("annotation list input is invalid")
	}
	return nil
}

// AnnotationPage is one ordered bounded page of live annotations.
type AnnotationPage struct {
	SessionID  string       `json:"sessionId"`
	Entries    []Annotation `json:"entries"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

// Validate checks a page and its strict annotation ordering.
func (p AnnotationPage) Validate() error {
	if !identifier.Valid(p.SessionID, "session_") || p.Entries == nil || len(p.Entries) > MaxAnnotationPageSize || len(p.NextCursor) > MaxAnnotationCursorBytes {
		return fmt.Errorf("annotation page is invalid")
	}
	var previous uint64
	for _, annotation := range p.Entries {
		if annotation.SessionID != p.SessionID || annotation.ID <= previous || annotation.Validate() != nil {
			return fmt.Errorf("annotation page entry is invalid")
		}
		previous = annotation.ID
	}
	return nil
}

// AnnotationSummary is the bounded snapshot projection used to restore chips and inline markers.
type AnnotationSummary struct {
	ID          uint64                `json:"id"`
	Anchor      AnnotationAnchor      `json:"anchor"`
	BodyPreview string                `json:"bodyPreview"`
	Preview     string                `json:"preview"`
	Stale       bool                  `json:"stale,omitempty"`
	StaleReason AnnotationStaleReason `json:"staleReason,omitempty"`
}

// Validate checks a bounded annotation summary.
func (s AnnotationSummary) Validate() error {
	if s.ID == 0 || s.Anchor.Validate() != nil || !validAnnotationText(s.BodyPreview, false) || len(s.BodyPreview) > MaxAnnotationSummaryTextBytes || len(s.Preview) > MaxAnnotationSummaryTextBytes || !validAnnotationText(s.Preview, true) {
		return fmt.Errorf("annotation summary is invalid")
	}
	if s.Stale != (s.StaleReason != "") || s.StaleReason != "" && s.StaleReason != AnnotationStaleWorkspace && s.StaleReason != AnnotationStaleTarget && s.StaleReason != AnnotationStaleFile && s.StaleReason != AnnotationStaleUnavailable {
		return fmt.Errorf("annotation summary stale state is invalid")
	}
	return nil
}

// SubmittedAnnotation is the immutable annotation snapshot owned by an accepted message.
type SubmittedAnnotation struct {
	OriginalAnnotationID uint64            `json:"originalAnnotationId"`
	Anchor               AnnotationAnchor  `json:"anchor"`
	Body                 string            `json:"body"`
	Preview              AnnotationPreview `json:"preview"`
}

// Validate checks an immutable submitted annotation snapshot.
func (a SubmittedAnnotation) Validate() error {
	if a.OriginalAnnotationID == 0 || a.Anchor.Validate() != nil || a.Preview.Validate(a.Anchor) != nil || !validAnnotationText(a.Body, false) || len(a.Body) > MaxAnnotationBodyBytes {
		return fmt.Errorf("submitted annotation is invalid")
	}
	encoded, err := json.Marshal(a)
	if err != nil || len(encoded) > MaxSubmittedAnnotationJSONBytes {
		return fmt.Errorf("submitted annotation exceeds its encoded bound")
	}
	return nil
}

func validAnnotationText(value string, allowEmpty bool) bool {
	if !utf8.ValidString(value) || len(value) == 0 && !allowEmpty || !allowEmpty && strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if character != '\n' && character != '\t' && (unicode.IsControl(character) || unicode.Is(unicode.Cf, character)) {
			return false
		}
	}
	return true
}
