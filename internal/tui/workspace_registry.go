package tui

import (
	"fmt"
	"path"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

type workspacePaneActivity uint8

const (
	workspacePaneActivityNone workspacePaneActivity = iota
	workspacePaneActivityRunning
	workspacePaneActivityWarning
	workspacePaneActivityError
	workspacePaneActivityCount
)

func (activity workspacePaneActivity) marker() string {
	switch activity {
	case workspacePaneActivityRunning:
		return spinnerFrames[0]
	case workspacePaneActivityWarning:
		return glyphCircleSlash
	case workspacePaneActivityError:
		return glyphCross
	default:
		return ""
	}
}

type workspacePanePresentation struct {
	KeyboardBlocked bool
	Active          bool
	Visible         bool
	Focused         bool
}

type workspacePaneDefinition struct {
	Kind      workspacePaneKind
	Closable  bool
	Identity  func(workspacePaneDescriptor) (workspacePaneIdentity, error)
	Label     func(shellSnapshot, workspacePaneDescriptor) string
	Available func(shellSnapshot, workspacePaneDescriptor) bool
	Activity  func(shellSnapshot, workspacePaneDescriptor) workspacePaneActivity
	Build     func(shellView, ui.Theme, workspacePaneDescriptor, workspacePanePresentation) ui.Widget
}

var workspacePaneDefinitions = map[workspacePaneKind]workspacePaneDefinition{
	workspacePaneScratchpad: {
		Kind: workspacePaneScratchpad, Closable: true,
		Identity: func(descriptor workspacePaneDescriptor) (workspacePaneIdentity, error) {
			if descriptor.ResourceID != "" || descriptor.WorkspaceID != "" || descriptor.Path != "" {
				return "", fmt.Errorf("scratchpad workspace identity is invalid")
			}
			return workspacePaneIdentity("scratchpad"), nil
		},
		Label:     func(shellSnapshot, workspacePaneDescriptor) string { return "Scratchpad" },
		Available: func(snapshot shellSnapshot, _ workspacePaneDescriptor) bool { return snapshot.ScratchpadAvailable },
		Activity: func(snapshot shellSnapshot, _ workspacePaneDescriptor) workspacePaneActivity {
			switch snapshot.Scratchpad.State {
			case scratchpadConflict:
				return workspacePaneActivityWarning
			case scratchpadSaveFailed:
				return workspacePaneActivityError
			default:
				return workspacePaneActivityNone
			}
		},
		Build: func(view shellView, _ ui.Theme, _ workspacePaneDescriptor, presentation workspacePanePresentation) ui.Widget {
			return workspaceScratchpadPane{
				Presentation: presentation, Scratchpad: view.Scratchpad, Editor: view.Snapshot.Scratchpad,
				OnChanged: view.Callbacks.ScratchpadChanged, OnRetry: view.Callbacks.RetryScratchpad,
				OnReview: view.Callbacks.ReviewScratchpad, OnKeep: view.Callbacks.KeepEditingScratchpad,
				OnUseShared: view.Callbacks.UseSharedScratchpad, OnReplace: view.Callbacks.ReplaceSharedScratchpad,
				OnFocus: view.Callbacks.FocusWorkspaceContent,
			}
		},
	},
	workspacePaneDiff: {
		Kind: workspacePaneDiff, Closable: true,
		Identity: func(descriptor workspacePaneDescriptor) (workspacePaneIdentity, error) {
			if descriptor.WorkspaceID == "" || descriptor.ResourceID != "diff" {
				return "", fmt.Errorf("workspace diff identity is required")
			}
			return workspacePaneIdentity("diff:" + descriptor.WorkspaceID), nil
		},
		Label:     func(shellSnapshot, workspacePaneDescriptor) string { return "Diff" },
		Available: func(shellSnapshot, workspacePaneDescriptor) bool { return true },
		Activity:  func(shellSnapshot, workspacePaneDescriptor) workspacePaneActivity { return workspacePaneActivityNone },
		Build: func(view shellView, _ ui.Theme, descriptor workspacePaneDescriptor, presentation workspacePanePresentation) ui.Widget {
			return workspaceDiffPane{
				Descriptor: descriptor, CurrentWorkspaceID: view.Snapshot.CurrentWorkspaceID,
				Diff: view.Diff, Presentation: presentation, Dispatch: view.WorkspaceDispatch,
				Annotations:        view.Snapshot.ComposerAnnotations,
				InitialWrapLines:   view.Snapshot.DiffWrapLines,
				OnWrapLinesChanged: view.Callbacks.SetDiffWrapLines,
				MouseGestures:      view.Callbacks.WorkspaceMouse,
				OnFocusRequest:     view.Callbacks.FocusWorkspaceContent,
				OnInputOwnerChanged: func(kind paneInputKind, active bool) bool {
					return view.Callbacks.PaneInputChanged != nil && view.Callbacks.PaneInputChanged(descriptor, kind, active)
				},
				OnCreateAnnotation: view.Callbacks.CreateAnnotation,
				OnLoadAnnotation:   view.Callbacks.LoadAnnotation,
				OnUpdateAnnotation: view.Callbacks.UpdateAnnotation,
				OnRemoveAnnotation: view.Callbacks.RemoveAnnotation,
				OnWarning:          view.Callbacks.ShowDiffWarning,
				OnNotice:           view.Callbacks.ShowDiffNotice,
			}
		},
	},
	workspacePaneFile: {
		Kind: workspacePaneFile, Closable: true,
		Identity: func(descriptor workspacePaneDescriptor) (workspacePaneIdentity, error) {
			if descriptor.WorkspaceID == "" {
				return "", fmt.Errorf("file workspace identity is required")
			}
			if err := protocol.ValidateWorkspacePath(descriptor.Path, false); err != nil {
				return "", fmt.Errorf("file canonical path is invalid: %w", err)
			}
			return workspacePaneIdentity("file:" + descriptor.WorkspaceID + ":" + descriptor.Path), nil
		},
		Label: func(_ shellSnapshot, descriptor workspacePaneDescriptor) string {
			return path.Base(descriptor.Path)
		},
		Available: func(shellSnapshot, workspacePaneDescriptor) bool { return true },
		Activity:  func(shellSnapshot, workspacePaneDescriptor) workspacePaneActivity { return workspacePaneActivityNone },
		Build: func(view shellView, _ ui.Theme, descriptor workspacePaneDescriptor, presentation workspacePanePresentation) ui.Widget {
			return workspaceFilePane{
				Descriptor: descriptor, CurrentWorkspaceID: view.Snapshot.CurrentWorkspaceID,
				Files: view.WorkspaceFiles, Presentation: presentation, Dispatch: view.WorkspaceDispatch,
				Annotations:        view.Snapshot.ComposerAnnotations,
				MouseGestures:      view.Callbacks.WorkspaceMouse,
				OnFocusRequest:     view.Callbacks.FocusWorkspaceContent,
				OnCreateAnnotation: view.Callbacks.CreateAnnotation,
				OnLoadAnnotation:   view.Callbacks.LoadAnnotation,
				OnUpdateAnnotation: view.Callbacks.UpdateAnnotation,
				OnRemoveAnnotation: view.Callbacks.RemoveAnnotation,
			}
		},
	},
	workspacePaneSubagentConversation: {
		Kind:     workspacePaneSubagentConversation,
		Closable: true,
		Identity: func(descriptor workspacePaneDescriptor) (workspacePaneIdentity, error) {
			if descriptor.ResourceID == "" {
				return "", fmt.Errorf("subagent conversation identity is required")
			}
			return workspacePaneIdentity("subagent:" + descriptor.ResourceID), nil
		},
		Label: func(snapshot shellSnapshot, descriptor workspacePaneDescriptor) string {
			return subagentConversationLabel(snapshot.SubagentConversations, descriptor.ResourceID)
		},
		Available: func(snapshot shellSnapshot, descriptor workspacePaneDescriptor) bool {
			for _, conversation := range snapshot.SubagentConversations {
				if conversation.ID == descriptor.ResourceID {
					return true
				}
			}
			return false
		},
		Activity: func(snapshot shellSnapshot, descriptor workspacePaneDescriptor) workspacePaneActivity {
			for _, conversation := range snapshot.SubagentConversations {
				if conversation.ID != descriptor.ResourceID {
					continue
				}
				switch conversation.State {
				case "queued", "running":
					return workspacePaneActivityRunning
				case "aborted", "interrupted":
					return workspacePaneActivityWarning
				case "failed":
					return workspacePaneActivityError
				default:
					return workspacePaneActivityNone
				}
			}
			return workspacePaneActivityNone
		},
		Build: func(view shellView, theme ui.Theme, descriptor workspacePaneDescriptor, presentation workspacePanePresentation) ui.Widget {
			return view.subagentTranscriptPane(theme, descriptor.ResourceID, presentation.Active)
		},
	},
}
