package tui

import (
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

// inputOwner is the shared shell precedence contract. Feature controllers own
// their data; this resolver owns which controller may receive input or appear.
type inputOwner uint8

const (
	inputBase inputOwner = iota
	inputFileMention
	inputSessionMention
	inputInteraction
	inputAuth
	inputPane
	inputBashHistory
	inputConfiguration
	inputSessionDetails
	inputMCPStatus
	inputAnnotations
	inputRename
	inputSessions
	inputSessionRename
	inputSessionDelete
	inputFiles
	inputTabs
	inputSubagents
	inputSubagentDismiss
	inputTheme
	inputReading
	inputPalette
)

func (o inputOwner) permitsRoot() bool {
	return o == inputBase || o == inputFileMention || o == inputSessionMention
}

func (o inputOwner) modal() bool      { return o >= inputAuth }
func (o inputOwner) trapsFocus() bool { return o.modal() || o == inputInteraction }
func (o inputOwner) root() inputOwner {
	switch o {
	case inputSessionRename, inputSessionDelete:
		return inputSessions
	case inputSubagentDismiss:
		return inputSubagents
	default:
		return o
	}
}

func (w shellSnapshot) inputOwner() inputOwner {
	if w.Phase == phaseAuthSelect || w.Phase == phaseAuthAPIKey || w.Phase == phaseAuthWaiting || w.Phase == phaseAuthBrowser {
		return inputAuth
	}
	// Child dialogs always belong above their parent. The remaining order is a
	// deterministic fallback for simultaneous state updates, not permission to
	// open unrelated root modals on top of each other.
	switch {
	case w.SubagentDismissID != "":
		return inputSubagentDismiss
	case w.SessionExplorer.Open && w.SessionExplorer.DeleteOpen:
		return inputSessionDelete
	case w.SessionExplorer.Open && w.SessionExplorer.RenameOpen:
		return inputSessionRename
	case w.PaletteOpen:
		return inputPalette
	case w.ReadingPickerOpen:
		return inputReading
	case w.ThemePicker.Open:
		return inputTheme
	case w.SubagentsOpen:
		return inputSubagents
	case w.WorkspacePickerOpen:
		return inputTabs
	case w.WorkspaceFilePicker.Open:
		return inputFiles
	case w.SessionExplorer.Open:
		return inputSessions
	case w.SessionRename.Open:
		return inputRename
	case w.AnnotationPicker.Open:
		return inputAnnotations
	case w.MCPStatusOpen:
		return inputMCPStatus
	case w.SessionDetailsOpen:
		return inputSessionDetails
	case w.ConfigurationPicker.Mode != configurationPickerClosed:
		return inputConfiguration
	case w.BashHistory.Open:
		return inputBashHistory
	case w.PaneInput.active(w):
		return inputPane
	case len(w.PendingInteractions) > 0:
		return inputInteraction
	case w.SessionMention.Open && composerOwnsWorkspaceInput(w.Workspace):
		return inputSessionMention
	case w.FileMention.Open && composerOwnsWorkspaceInput(w.Workspace):
		return inputFileMention
	default:
		return inputBase
	}
}

func (s *appState) inputOwner() inputOwner {
	return (shellSnapshot{
		Phase: s.phase, Session: s.session, Workspace: s.workspace.Snapshot(), CurrentWorkspaceID: s.workspaceID, PaneInput: s.paneInput, PaletteOpen: s.palette.Open, ThemePicker: s.themePicker.Snapshot(),
		SubagentDismissID: s.subagentDismissID, SubagentsOpen: s.subagentsOpen,
		WorkspacePickerOpen: s.workspacePickerOpen, WorkspaceFilePicker: s.workspaceFilePicker,
		SessionExplorer: s.sessionExplorer.Snapshot(), SessionRename: s.sessionRename.Snapshot(),
		AnnotationPicker:   annotationPickerSnapshot{Open: s.annotationPicker.Open},
		SessionDetailsOpen: s.sessionDetailsOpen, MCPStatusOpen: s.mcpStatusOpen, ConfigurationPicker: s.configurationPicker.Snapshot(),
		BashHistory: s.bashHistory, SessionMention: s.sessionMention, FileMention: s.fileMention,
		PendingInteractions: s.pendingInteractions, ReadingPickerOpen: s.transcriptReadingPickerOpen || s.subagentReadingPickerID != "",
	}).inputOwner()
}

func (s *appState) canOpenRootModal() bool {
	return s.phase == phaseReady && (s.inputOwner().permitsRoot() || (s.replacingPalette && s.inputOwner() == inputInteraction))
}

func (o inputOwner) acceptsText() bool {
	switch o {
	case inputAnnotations, inputSubagentDismiss, inputSessionDelete, inputSessionDetails, inputMCPStatus, inputTheme:
		return false
	default:
		return true
	}
}

// Tokens bind a bracketed paste to the same session, owner, and selected pane.
type inputToken struct {
	owner                inputOwner
	generation           uint64
	operation            uint64
	controllerGeneration uint64
	control              string
	sessionID            string
	interactionID        string
	pane                 workspacePaneIdentity
	focus                workspaceFocusOwner
}

func (s *appState) inputToken() inputToken {
	token := inputToken{owner: s.inputOwner(), generation: s.inputGeneration, operation: s.operation, sessionID: s.session.ID, pane: s.workspace.SelectedIdentity(), focus: s.workspace.FocusOwner()}
	switch token.owner {
	case inputFiles:
		token.controllerGeneration = s.workspaceFilePicker.Generation
	case inputSessions, inputSessionRename, inputSessionDelete:
		token.controllerGeneration = s.sessionExplorer.generation
		token.control = s.sessionExplorer.RenameSessionID
	case inputRename:
		token.controllerGeneration = s.sessionRename.generation
		token.control = s.sessionRename.SessionID
	case inputConfiguration:
		token.controllerGeneration = s.configurationPicker.generation
		token.control = s.configurationPicker.EditModel
	case inputTheme:
		token.controllerGeneration = s.themeGeneration
	case inputAuth:
		token.controllerGeneration = s.loginGeneration
		token.control = s.authProviderID
	case inputPane:
		token.controllerGeneration = s.paneInput.Generation
		token.control = string(s.paneInput.Pane)
	}
	if len(s.pendingInteractions) > 0 {
		token.interactionID = s.pendingInteractions[0].ID
	}
	return token
}
func (s *appState) reconcileInputOwner() {
	snapshot := shellSnapshot{Phase: s.phase, Session: s.session, CurrentWorkspaceID: s.workspaceID, Workspace: s.workspace.Snapshot(), PaneInput: s.paneInput}
	if s.paneInput.Kind != paneInputNone && !s.paneInput.active(snapshot) {
		s.paneInput = paneInputOwner{}
		s.inputGeneration++
	}
	if len(s.pendingInteractions) > 0 || !composerOwnsWorkspaceInput(s.workspace.Snapshot()) {
		s.fileMention.Close()
		if s.sessionMention.Open {
			s.closeSessionMention()
		}
		s.bashHistory.Close()
	}
	owner := s.inputOwner()
	view := shellView{Snapshot: shellSnapshot{Session: s.session, CurrentWorkspaceID: s.workspaceID, Workspace: s.workspace.Snapshot()}}
	if owner.trapsFocus() && !s.previousInputOwner.trapsFocus() {
		s.inputReturn = captureShellFocus(view)
		if s.inputControl.valid(view) {
			s.inputReturn = s.inputControl.region
		}
	}
	if !owner.trapsFocus() && s.previousInputOwner.trapsFocus() {
		focus := workspaceFocusComposer
		if s.inputReturn.validContent(view) {
			focus = workspaceFocusContent
		}
		s.workspace.SetFocusOwner(focus)
	}
	if owner != s.previousInputOwner {
		s.previousInputOwner = owner
		s.inputGeneration++
	}
}

// admitRootModal invalidates in-flight input even for close/reopen transitions
// that happen before the next frame observes an intermediate base owner.
func (s *appState) admitRootModal() bool {
	if !s.canOpenRootModal() {
		return false
	}
	s.inputGeneration++
	s.fileMention.Close()
	if s.sessionMention.Open {
		s.closeSessionMention()
	}
	return true
}

// deliverPaste never dispatches pasted Enter/Space to a button or shortcut.
// Feature-owned query editors can handle pre-frame input directly; fallback
// insertion is allowed only after this owner has actually been rendered.
func (s *appState) deliverPaste(ctx ui.EventContext, key ui.Key) ui.EventResult {
	if s.handleKey(ctx, key) == ui.EventHandled {
		return ui.EventHandled
	}
	token := s.inputToken()
	if s.renderedInput == token && s.targetOwnsInput(ctx) {
		insertPastedText(ctx, pastedKeyText(key))
	}
	return ui.EventHandled
}

func (s *appState) acceptsPaste(owner inputOwner) bool {
	if !owner.acceptsText() {
		return false
	}
	switch owner {
	case inputAuth:
		return !s.authPending && (s.phase == phaseAuthSelect || s.phase == phaseAuthAPIKey || s.phase == phaseAuthBrowser)
	case inputConfiguration:
		return !s.configurationPicker.Pending && s.configurationPicker.Mode == configurationPickerModel
	case inputRename:
		return !s.sessionRename.Pending
	case inputSessionRename:
		return !s.sessionExplorer.RenamePending
	case inputInteraction:
		return len(s.pendingInteractions) > 0 && (s.pendingInteractions[0].Kind == protocol.InteractionInput || s.pendingInteractions[0].Kind == protocol.InteractionGuided)
	default:
		return true
	}
}

func composerOwnsWorkspaceInput(workspace workspaceControllerSnapshot) bool {
	return workspace.FocusOwner == workspaceFocusComposer || workspace.Selected == "" || workspace.Selected == workspaceAgentIdentity
}

// inputTargetIntent identifies the rendered layer containing the event target.
// This is separate from logical ownership: mouse selection may focus background
// content without granting it keyboard or paste permission.
type inputTargetIntent struct{ Report func(inputOwner) }

func (inputTargetIntent) IntentType() ui.IntentType { return "kit.input-target" }
func inputTargetAction(owner inputOwner) ui.ActionFunc {
	return func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
		intent.(inputTargetIntent).Report(owner)
		return ui.EventHandled
	}
}
func renderedInputTarget(ctx ui.EventContext) inputOwner {
	target := inputBase
	ctx.Invoke(inputTargetIntent{Report: func(owner inputOwner) { target = owner }})
	return target
}
func (s *appState) targetOwnsInput(ctx ui.EventContext) bool {
	target := renderedInputTarget(ctx)
	owner := s.inputOwner()
	return target == owner || (target == inputBase && (owner == inputFileMention || owner == inputSessionMention))
}
