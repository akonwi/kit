package tui

import (
	"errors"
	"fmt"
	"strings"
)

const workspaceSecondaryPaneLimit = 32

var errWorkspacePaneLimit = errors.New("workspace pane limit reached")

type workspacePaneKind string

const (
	workspacePaneSubagentConversation workspacePaneKind = "subagent-conversation"
	workspacePaneFile                 workspacePaneKind = "file"
	workspacePaneDiff                 workspacePaneKind = "diff"
	workspacePaneScratchpad           workspacePaneKind = "scratchpad"
)

var workspacePaneKinds = [...]workspacePaneKind{
	workspacePaneSubagentConversation,
	workspacePaneFile,
	workspacePaneDiff,
	workspacePaneScratchpad,
}

// workspacePaneDescriptor is renderer-neutral client state. It identifies a
// retained resource without storing widgets, callbacks, focus nodes, or runtime
// lifecycle.
type workspacePaneDescriptor struct {
	Kind                 workspacePaneKind
	ResourceID           string
	WorkspaceID          string
	Path                 string
	ExpectedRevision     string
	ExpectedFileRevision string
	DiffTargetID         string
	DiffSide             string
	AnnotationID         uint64
	RevealStartLine      int
	RevealEndLine        int
	OpenGeneration       uint64
}

func subagentWorkspacePane(conversationID string) workspacePaneDescriptor {
	return workspacePaneDescriptor{Kind: workspacePaneSubagentConversation, ResourceID: conversationID}
}

func fileWorkspacePane(workspaceID, path string) workspacePaneDescriptor {
	return workspacePaneDescriptor{Kind: workspacePaneFile, WorkspaceID: workspaceID, Path: path}
}

func workingTreeDiffWorkspacePane(workspaceID string) workspacePaneDescriptor {
	return workspacePaneDescriptor{Kind: workspacePaneDiff, WorkspaceID: workspaceID, ResourceID: "diff"}
}

type workspacePaneIdentity string

const workspaceAgentIdentity workspacePaneIdentity = "agent"

type workspaceControllerSnapshot struct {
	Panes      []workspacePaneDescriptor
	Selected   workspacePaneIdentity
	FocusOwner workspaceFocusOwner
}

func (s workspaceControllerSnapshot) StripVisible() bool { return len(s.Panes) > 0 }

func (s workspaceControllerSnapshot) SelectedPane() (workspacePaneDescriptor, bool) {
	if s.Selected == "" || s.Selected == workspaceAgentIdentity {
		return workspacePaneDescriptor{}, false
	}
	for _, pane := range s.Panes {
		identity, err := workspacePaneIdentityFor(pane)
		if err == nil && identity == s.Selected {
			return pane, true
		}
	}
	return workspacePaneDescriptor{}, false
}

type workspaceFocusOwner uint8

const (
	workspaceFocusContent workspaceFocusOwner = iota
	workspaceFocusComposer
)

// workspaceController owns the ordered secondary panes, selected surface, and
// logical shell focus for one attached session view. Its zero value is an
// Agent-only workspace focused on content.
type workspaceController struct {
	panes          []workspacePaneDescriptor
	selected       workspacePaneIdentity
	focusOwner     workspaceFocusOwner
	secondaryLimit int
	openGeneration uint64
}

func (c *workspaceController) Panes() []workspacePaneDescriptor {
	return append([]workspacePaneDescriptor(nil), c.panes...)
}

func (c *workspaceController) Snapshot() workspaceControllerSnapshot {
	return workspaceControllerSnapshot{
		Panes: c.Panes(), Selected: c.SelectedIdentity(), FocusOwner: c.FocusOwner(),
	}
}

func (c *workspaceController) StripVisible() bool { return len(c.panes) > 0 }

func (c *workspaceController) SelectedIdentity() workspacePaneIdentity {
	if c.selected == "" {
		return workspaceAgentIdentity
	}
	return c.selected
}

func (c *workspaceController) SelectedPane() (workspacePaneDescriptor, bool) {
	if c.selected == "" {
		return workspacePaneDescriptor{}, false
	}
	for _, pane := range c.panes {
		identity, err := workspacePaneIdentityFor(pane)
		if err == nil && identity == c.selected {
			return pane, true
		}
	}
	return workspacePaneDescriptor{}, false
}

func (c *workspaceController) Open(descriptor workspacePaneDescriptor) (workspacePaneIdentity, bool, error) {
	descriptor.ResourceID = strings.TrimSpace(descriptor.ResourceID)
	descriptor.WorkspaceID = strings.TrimSpace(descriptor.WorkspaceID)
	identity, err := workspacePaneIdentityFor(descriptor)
	if err != nil {
		return "", false, err
	}
	if descriptor.Kind == workspacePaneFile || descriptor.Kind == workspacePaneDiff {
		c.openGeneration++
		descriptor.OpenGeneration = c.openGeneration
	}
	for index, pane := range c.panes {
		candidate, candidateErr := workspacePaneIdentityFor(pane)
		if candidateErr == nil && candidate == identity {
			c.panes[index] = descriptor
			c.selected = identity
			c.focusOwner = workspaceFocusContent
			return identity, false, nil
		}
	}
	limit := c.secondaryLimit
	if limit <= 0 {
		limit = workspaceSecondaryPaneLimit
	}
	if len(c.panes) >= limit {
		return "", false, fmt.Errorf("%w: close a tab before opening another", errWorkspacePaneLimit)
	}
	c.panes = append(c.panes, descriptor)
	c.selected = identity
	c.focusOwner = workspaceFocusContent
	return identity, true, nil
}

func (c *workspaceController) SelectAgent() {
	c.selected = ""
	c.focusOwner = workspaceFocusContent
}

func (c *workspaceController) Select(identity workspacePaneIdentity) bool {
	if identity == workspaceAgentIdentity || identity == "" {
		c.SelectAgent()
		return true
	}
	for _, pane := range c.panes {
		candidate, err := workspacePaneIdentityFor(pane)
		if err == nil && candidate == identity {
			c.selected = identity
			c.focusOwner = workspaceFocusContent
			return true
		}
	}
	return false
}

func (c *workspaceController) MoveSelection(delta int) bool {
	count := len(c.panes) + 1
	if count <= 1 || delta == 0 {
		return false
	}
	current := 0
	if c.selected != "" {
		for index, pane := range c.panes {
			identity, err := workspacePaneIdentityFor(pane)
			if err == nil && identity == c.selected {
				current = index + 1
				break
			}
		}
	}
	next := (current + delta) % count
	if next < 0 {
		next += count
	}
	if next == 0 {
		c.SelectAgent()
		return true
	}
	identity, err := workspacePaneIdentityFor(c.panes[next-1])
	if err != nil {
		return false
	}
	return c.Select(identity)
}

func (c *workspaceController) Close(identity workspacePaneIdentity) bool {
	if identity == "" || identity == workspaceAgentIdentity {
		return false
	}
	index := -1
	for paneIndex, pane := range c.panes {
		candidate, err := workspacePaneIdentityFor(pane)
		if err == nil && candidate == identity {
			index = paneIndex
			break
		}
	}
	if index < 0 {
		return false
	}
	wasSelected := c.selected == identity
	c.panes = append(c.panes[:index], c.panes[index+1:]...)
	if !wasSelected {
		return true
	}
	if len(c.panes) == 0 {
		c.selected = ""
		return true
	}
	nextIndex := min(index, len(c.panes)-1)
	nextIdentity, err := workspacePaneIdentityFor(c.panes[nextIndex])
	if err != nil {
		c.selected = ""
		return true
	}
	c.selected = nextIdentity
	return true
}

func (c *workspaceController) FocusOwner() workspaceFocusOwner { return c.focusOwner }

func (c *workspaceController) SetFocusOwner(owner workspaceFocusOwner) {
	if owner != workspaceFocusComposer {
		owner = workspaceFocusContent
	}
	c.focusOwner = owner
}

func (c *workspaceController) MoveFocus() workspaceFocusOwner {
	if c.focusOwner == workspaceFocusComposer {
		c.focusOwner = workspaceFocusContent
	} else {
		c.focusOwner = workspaceFocusComposer
	}
	return c.focusOwner
}

func (c *workspaceController) Reset() {
	c.panes = nil
	c.selected = ""
	c.focusOwner = workspaceFocusContent
	c.openGeneration = 0
}

func workspacePaneIdentityFor(descriptor workspacePaneDescriptor) (workspacePaneIdentity, error) {
	definition, ok := workspacePaneDefinitions[descriptor.Kind]
	if !ok {
		return "", fmt.Errorf("unsupported workspace pane kind %q", descriptor.Kind)
	}
	descriptor.ResourceID = strings.TrimSpace(descriptor.ResourceID)
	descriptor.WorkspaceID = strings.TrimSpace(descriptor.WorkspaceID)
	return definition.Identity(descriptor)
}
