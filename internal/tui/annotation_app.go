package tui

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

func (s *appState) applyAnnotationEvent(event protocol.SessionEvent) {
	switch event.Kind {
	case protocol.SessionEventAnnotationCreated, protocol.SessionEventAnnotationUpdated:
		if event.Annotation == nil {
			return
		}
		summary := protocol.AnnotationSummary{
			ID: event.Annotation.ID, Anchor: event.Annotation.Anchor,
			BodyPreview: annotationSummaryText(event.Annotation.Body), Preview: annotationSummaryText(event.Annotation.Preview.Text),
			Stale: event.Annotation.Stale, StaleReason: event.Annotation.StaleReason,
		}
		for index := range s.annotations {
			if s.annotations[index].ID == summary.ID {
				s.annotations[index] = summary
				return
			}
		}
		s.annotations = append(s.annotations, summary)
		sort.Slice(s.annotations, func(i, j int) bool { return s.annotations[i].ID < s.annotations[j].ID })
	case protocol.SessionEventAnnotationDeleted:
		s.removeAnnotationSummary(event.AnnotationID)
	case protocol.SessionEventAnnotationSubmitted:
		for _, id := range event.AnnotationIDs {
			s.removeAnnotationSummary(id)
		}
	}
}

func annotationErrorText(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	var userMessage interface{ UserMessage() string }
	if errors.As(err, &userMessage) {
		message = userMessage.UserMessage()
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "annotation operation failed"
	}
	return message
}

func annotationSummaryText(value string) string {
	if len(value) <= protocol.MaxAnnotationSummaryTextBytes {
		return value
	}
	value = value[:protocol.MaxAnnotationSummaryTextBytes]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (s *appState) removeAnnotationSummary(id uint64) {
	for index := range s.annotations {
		if s.annotations[index].ID == id {
			s.annotations = append(s.annotations[:index], s.annotations[index+1:]...)
			if len(s.annotations) == 0 {
				s.annotationPicker.Close()
			} else {
				s.annotationPicker.Selection = min(s.annotationPicker.Selection, len(s.annotations)-1)
			}
			return
		}
	}
}

func (s *appState) annotationIDs() []uint64 {
	ids := make([]uint64, 0, len(s.annotations))
	for _, annotation := range s.annotations {
		ids = append(ids, annotation.ID)
	}
	return ids
}

func (s *appState) activateAnnotation(annotation protocol.AnnotationSummary) {
	if annotation.Stale {
		for index := range s.annotations {
			if s.annotations[index].ID == annotation.ID {
				s.SetState(func() {
					s.annotationPicker.Begin()
					s.annotationPicker.Selection = index
				})
				return
			}
		}
		return
	}
	descriptor := workspacePaneDescriptor{}
	if anchor := annotation.Anchor.WorkspaceFile; anchor != nil {
		descriptor = fileWorkspacePane(anchor.WorkspaceID, anchor.Path)
		descriptor.ExpectedRevision = anchor.FileRevision
		descriptor.RevealStartLine = anchor.StartLine
		descriptor.RevealEndLine = anchor.EndLine
	} else if anchor := annotation.Anchor.WorkingTreeDiff; anchor != nil {
		descriptor = workingTreeDiffWorkspacePane(s.workspaceID)
		descriptor.Path = anchor.Path
		descriptor.DiffTargetID = anchor.TargetID
		descriptor.ExpectedRevision = anchor.TargetRevision
		descriptor.ExpectedFileRevision = anchor.FileRevision
		descriptor.DiffSide = anchor.Side
		descriptor.RevealStartLine = anchor.StartLine
		descriptor.RevealEndLine = anchor.EndLine
	} else {
		return
	}
	if _, _, err := s.workspace.Open(descriptor); err != nil {
		s.showToast(toastInput{Title: "Could not open annotation", Subtitle: err.Error(), Variant: toastWarning})
		return
	}
	s.syncWorkspaceSelection()
}

func (s *appState) loadInlineAnnotation(annotationID uint64, done func(string, error)) func() {
	annotations, ok := s.bound.(sessionclient.AnnotationSession)
	if !ok {
		done("", errors.New("annotations are unavailable"))
		return func() {}
	}
	ctx, cancel := context.WithCancel(s.ctx)
	runtime := s.Context().Runtime()
	appOperation, bound, sessionID := s.operation, s.bound, s.session.ID
	go func() {
		defer cancel()
		cursor := ""
		for {
			page, err := annotations.ListAnnotations(ctx, protocol.ListAnnotationsInput{Cursor: cursor, PageSize: protocol.MaxAnnotationPageSize})
			if err != nil {
				if context.Cause(ctx) != nil {
					return
				}
				runtime.Dispatch(func() {
					if appOperation == s.operation && bound == s.bound && sessionID == s.session.ID {
						done("", err)
					}
				})
				return
			}
			for _, annotation := range page.Entries {
				if annotation.ID == annotationID {
					body := annotation.Body
					runtime.Dispatch(func() {
						if appOperation == s.operation && bound == s.bound && sessionID == s.session.ID {
							done(body, nil)
						}
					})
					return
				}
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		runtime.Dispatch(func() {
			if appOperation == s.operation && bound == s.bound && sessionID == s.session.ID {
				done("", errors.New("annotation no longer exists"))
			}
		})
	}()
	return cancel
}

func (s *appState) updateInlineAnnotation(annotationID uint64, body string, done func(error)) {
	annotations, ok := s.bound.(sessionclient.AnnotationSession)
	if !ok {
		done(errors.New("annotations are unavailable"))
		return
	}
	ctx, runtime := s.ctx, s.Context().Runtime()
	appOperation, bound, sessionID := s.operation, s.bound, s.session.ID
	go func() {
		updated, err := annotations.UpdateAnnotation(ctx, protocol.UpdateAnnotationInput{AnnotationID: annotationID, Body: body})
		if context.Cause(ctx) != nil {
			return
		}
		runtime.Dispatch(func() {
			if appOperation != s.operation || bound != s.bound || sessionID != s.session.ID {
				return
			}
			if err == nil {
				s.SetState(func() {
					s.applyAnnotationEvent(protocol.SessionEvent{Kind: protocol.SessionEventAnnotationUpdated, Annotation: &updated})
				})
			}
			done(err)
		})
	}()
}

func (s *appState) createInlineAnnotation(anchor protocol.AnnotationAnchor, body string, done func(error)) {
	annotations, ok := s.bound.(sessionclient.AnnotationSession)
	if !ok {
		if done != nil {
			done(errors.New("annotations are unavailable"))
		}
		return
	}
	input := protocol.CreateAnnotationInput{Anchor: anchor, Body: body}
	ctx, runtime := s.ctx, s.Context().Runtime()
	appOperation, bound, sessionID := s.operation, s.bound, s.session.ID
	go func() {
		created, err := annotations.CreateAnnotation(ctx, input)
		if context.Cause(ctx) != nil {
			return
		}
		runtime.Dispatch(func() {
			if appOperation != s.operation || bound != s.bound || sessionID != s.session.ID {
				return
			}
			if err == nil {
				s.SetState(func() {
					s.applyAnnotationEvent(protocol.SessionEvent{Kind: protocol.SessionEventAnnotationCreated, Annotation: &created})
				})
			}
			if done != nil {
				done(err)
			}
		})
	}()
}

func (s *appState) reanchorAnnotation(annotation protocol.AnnotationSummary) {
	descriptor := workspacePaneDescriptor{}
	if anchor := annotation.Anchor.WorkspaceFile; anchor != nil {
		workspaceID := s.workspaceID
		if workspaceID == "" {
			workspaceID = anchor.WorkspaceID
		}
		descriptor = fileWorkspacePane(workspaceID, anchor.Path)
		descriptor.RevealStartLine = anchor.StartLine
		descriptor.RevealEndLine = anchor.EndLine
	} else if anchor := annotation.Anchor.WorkingTreeDiff; anchor != nil {
		descriptor = workingTreeDiffWorkspacePane(s.workspaceID)
		descriptor.Path = anchor.Path
		descriptor.RevealStartLine = anchor.StartLine
		descriptor.RevealEndLine = anchor.EndLine
	} else {
		return
	}
	if _, _, err := s.workspace.Open(descriptor); err != nil {
		s.showToast(toastInput{Title: "Could not re-anchor annotation", Subtitle: err.Error(), Variant: toastWarning})
		return
	}
	s.annotationPicker.Close()
	s.syncWorkspaceSelection()
	s.showToast(toastInput{Title: "Select current source", Subtitle: "Adjust the range, press c, then remove the stale annotation.", Variant: toastWarning})
}

func (s *appState) removeAnnotation(annotationID uint64) {
	annotations, ok := s.bound.(sessionclient.AnnotationSession)
	if !ok || s.session.ID == "" || annotationID == 0 {
		return
	}
	ctx, runtime := s.ctx, s.Context().Runtime()
	appOperation, bound, sessionID := s.operation, s.bound, s.session.ID
	go func() {
		err := annotations.DeleteAnnotation(ctx, protocol.DeleteAnnotationInput{AnnotationID: annotationID})
		if context.Cause(ctx) != nil {
			return
		}
		runtime.Dispatch(func() {
			if appOperation != s.operation || bound != s.bound || sessionID != s.session.ID {
				return
			}
			if err != nil {
				s.showToast(toastInput{Title: "Could not remove annotation", Subtitle: err.Error(), Variant: toastError})
			}
		})
	}()
}
