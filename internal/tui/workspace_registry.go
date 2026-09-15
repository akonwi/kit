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
	Active  bool
	Visible bool
	Focused bool
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
		Build: func(_ shellView, theme ui.Theme, descriptor workspacePaneDescriptor, _ workspacePanePresentation) ui.Widget {
			return ui.Center(ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
				ui.Text{Value: descriptor.Path, Style: ui.Style{Foreground: theme.Foreground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
				ui.Text{Value: "File viewer is coming in the next slice.", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
			}})
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
