package sessionbridge

import (
	"context"
	"errors"
	"unicode/utf8"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

// Annotation is the Ard client's flat projection of a live annotation: what
// its chips, inline notes, and gutter markers need. Kind is "file" for a
// workspace-file range or "diff" for a side of a diff observation.
type Annotation struct {
	ID   uint64
	Kind string
	// WorkspaceID identifies a file annotation's workspace.
	WorkspaceID string
	// TargetID and TargetRevision identify a diff annotation's observation,
	// and Side is its "old" or "new" side.
	TargetID       string
	TargetRevision string
	Side           string
	Path           string
	FileRevision   string
	StartLine      int
	EndLine        int
	// Body is the note, bounded like a snapshot's body preview; Preview is the
	// start of the annotated source.
	Body               string
	Preview            string
	Stale              bool
	StaleReason        string
	ValidationDeferred bool
}

// Summaries projects a snapshot's live annotations.
func Summaries(source []protocol.AnnotationSummary) []Annotation {
	result := make([]Annotation, 0, len(source))
	for _, summary := range source {
		result = append(result, summaryAnnotation(summary))
	}
	return result
}

func summaryAnnotation(summary protocol.AnnotationSummary) Annotation {
	projected := anchored(summary.Anchor)
	projected.ID = summary.ID
	projected.Body, projected.Preview = summary.BodyPreview, summary.Preview
	projected.Stale, projected.StaleReason = summary.Stale, string(summary.StaleReason)
	projected.ValidationDeferred = summary.ValidationDeferred
	return projected
}

// Record projects a full annotation, as created and updated events carry it,
// bounding its body and preview as a snapshot would.
func Record(annotation protocol.Annotation) Annotation {
	return summaryAnnotation(protocol.AnnotationSummary{
		ID: annotation.ID, Anchor: annotation.Anchor, DiffTarget: annotation.DiffTarget,
		BodyPreview: bounded(annotation.Body), Preview: bounded(annotation.Preview.Text),
		Stale: annotation.Stale, StaleReason: annotation.StaleReason, ValidationDeferred: annotation.ValidationDeferred,
	})
}

func anchored(anchor protocol.AnnotationAnchor) Annotation {
	if file := anchor.WorkspaceFile; file != nil {
		return Annotation{Kind: "file", WorkspaceID: file.WorkspaceID, Path: file.Path, FileRevision: file.FileRevision, StartLine: file.StartLine, EndLine: file.EndLine}
	}
	if diff := anchor.WorkingTreeDiff; diff != nil {
		return Annotation{Kind: "diff", TargetID: diff.TargetID, TargetRevision: diff.TargetRevision, Side: diff.Side, Path: diff.Path, FileRevision: diff.FileRevision, StartLine: diff.StartLine, EndLine: diff.EndLine}
	}
	return Annotation{}
}

// bounded cuts text to the summary bound without splitting a character.
func bounded(value string) string {
	if len(value) <= protocol.MaxAnnotationSummaryTextBytes {
		return value
	}
	value = value[:protocol.MaxAnnotationSummaryTextBytes]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

// FileAnchor anchors an annotation to lines of a workspace file revision.
func FileAnchor(workspaceID, path, fileRevision string, startLine, endLine int) protocol.AnnotationAnchor {
	return protocol.AnnotationAnchor{
		Kind: protocol.AnnotationAnchorWorkspaceFile,
		WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
			WorkspaceID: workspaceID, Path: path, FileRevision: fileRevision, StartLine: startLine, EndLine: endLine,
		},
	}
}

// DiffAnchor anchors an annotation to lines of one side of a diff observation.
func DiffAnchor(targetID, targetRevision, path, fileRevision, side string, startLine, endLine int) protocol.AnnotationAnchor {
	return protocol.AnnotationAnchor{
		Kind: protocol.AnnotationAnchorWorkingTreeDiff,
		WorkingTreeDiff: &protocol.WorkingTreeDiffAnnotationAnchor{
			TargetID: targetID, TargetRevision: targetRevision, Path: path, FileRevision: fileRevision, Side: side, StartLine: startLine, EndLine: endLine,
		},
	}
}

// ErrAnnotationGone reports that an annotation was deleted or sent.
var ErrAnnotationGone = errors.New("annotation no longer exists")

// AnnotationBody reads an annotation's full body, which snapshots bound, by
// listing the session's annotations until it is found.
func AnnotationBody(ctx context.Context, session *kit.Session, id uint64) (string, error) {
	cursor := ""
	for {
		page, err := session.ListAnnotations(ctx, protocol.ListAnnotationsInput{Cursor: cursor, PageSize: protocol.MaxAnnotationPageSize})
		if err != nil {
			return "", err
		}
		for _, annotation := range page.Entries {
			if annotation.ID == id {
				return annotation.Body, nil
			}
		}
		if page.NextCursor == "" {
			return "", ErrAnnotationGone
		}
		cursor = page.NextCursor
	}
}
