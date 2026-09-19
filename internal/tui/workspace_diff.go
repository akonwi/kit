package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/highlight"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	kittheme "github.com/akonwi/kit/internal/theme"
	"github.com/rockorager/go-uucode"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

type workspaceDiffPhase uint8

type workspaceDiffSide uint8

const (
	workspaceDiffSideOld workspaceDiffSide = iota
	workspaceDiffSideNew
)

const (
	workspaceDiffSplitBreakpoint = 120
	workspaceDiffRefreshInterval = 30 * time.Second
)

const (
	workspaceDiffInitial workspaceDiffPhase = iota
	workspaceDiffLoading
	workspaceDiffReady
	workspaceDiffEmpty
	workspaceDiffError
)

type moveWorkspaceDiffIntent struct{ lines, columns int }

func (moveWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.move" }

type moveWorkspaceDiffFileIntent struct{ delta int }

func (moveWorkspaceDiffFileIntent) IntentType() ui.IntentType { return "kit.workspace-diff.file" }

type moveWorkspaceDiffHunkIntent struct{ delta int }

func (moveWorkspaceDiffHunkIntent) IntentType() ui.IntentType { return "kit.workspace-diff.hunk" }

type refreshWorkspaceDiffIntent struct{}

func (refreshWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.refresh" }

type loadMoreWorkspaceDiffIntent struct{}

func (loadMoreWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.load-more" }

type toggleWorkspaceDiffLayoutIntent struct{}

func (toggleWorkspaceDiffLayoutIntent) IntentType() ui.IntentType {
	return "kit.workspace-diff.toggle-layout"
}

type toggleWorkspaceDiffWrapIntent struct{}

func (toggleWorkspaceDiffWrapIntent) IntentType() ui.IntentType {
	return "kit.workspace-diff.toggle-wrap"
}

type panWorkspaceDiffIntent struct{ columns int }

func (panWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.pan" }

type commentWorkspaceDiffIntent struct{}

func (commentWorkspaceDiffIntent) IntentType() ui.IntentType { return "kit.workspace-diff.comment" }

type editWorkspaceDiffAnnotationIntent struct{}

func (editWorkspaceDiffAnnotationIntent) IntentType() ui.IntentType {
	return "kit.workspace-diff.edit-annotation"
}

type removeWorkspaceDiffAnnotationIntent struct{}

func (removeWorkspaceDiffAnnotationIntent) IntentType() ui.IntentType {
	return "kit.workspace-diff.remove-annotation"
}

type selectWorkspaceDiffRangeIntent struct{}

func (selectWorkspaceDiffRangeIntent) IntentType() ui.IntentType {
	return "kit.workspace-diff.select-range"
}

type toggleWorkspaceDiffTargetIntent struct{}

func (toggleWorkspaceDiffTargetIntent) IntentType() ui.IntentType {
	return "kit.workspace-diff.toggle-target"
}

type openWorkspaceDiffTargetPickerIntent struct{}

func (openWorkspaceDiffTargetPickerIntent) IntentType() ui.IntentType {
	return "kit.workspace-diff.open-target-picker"
}

type moveWorkspaceDiffTargetIntent struct{ delta int }

func (moveWorkspaceDiffTargetIntent) IntentType() ui.IntentType {
	return "kit.workspace-diff.move-target"
}

type workspaceDiffPane struct {
	Descriptor         workspacePaneDescriptor
	CurrentWorkspaceID string
	Diff               sessionclient.DiffSession
	Highlighter        highlight.Highlighter
	Dispatch           func(func())
	Presentation       workspacePanePresentation
	Annotations        []protocol.AnnotationSummary
	InitialWrapLines   bool
	OnWrapLinesChanged func(bool)
	MouseGestures      *workspaceMouseGestureController
	OnFocusRequest     ui.VoidCallback
	OnCreateAnnotation func(protocol.AnnotationAnchor, string, func(error))
	OnLoadAnnotation   func(uint64, func(string, error)) func()
	OnUpdateAnnotation func(uint64, string, func(error))
	OnRemoveAnnotation func(ui.EventContext, uint64)
	OnWarning          func(string)
	OnNotice           func(string)
	RefreshInterval    time.Duration
	testState          *workspaceDiffPaneState
}

func (w workspaceDiffPane) CreateState() ui.State {
	if w.testState != nil {
		return w.testState
	}
	return &workspaceDiffPaneState{}
}

type workspaceDiffObservationResult struct {
	generation uint64
	page       protocol.WorkingTreePage
	err        error
}

type workspaceDiffFileResult struct {
	generation uint64
	page       protocol.FileDiffPage
	append     bool
	err        error
}

type workspaceDiffHighlightResult struct {
	generation uint64
	old        highlight.Result
	new        highlight.Result
}

type workspaceDiffPollResult struct {
	generation uint64
	page       protocol.DiffPage
	err        error
}

type workspaceDiffCatalogResult struct {
	generation uint64
	catalog    protocol.DiffTargetCatalog
	err        error
}

type workspaceDiffPaneState struct {
	ui.StateBase
	active                  *workspaceDiffFileState
	sections                []*workspaceDiffFileState
	readWork                workspaceDiffWorkPool
	highlightWork           workspaceDiffWorkPool
	unifiedLayout           bool
	layoutReady             bool
	scrollCorrection        int
	lastViewportOffset      int
	jumpToFile              bool
	phase                   workspaceDiffPhase
	generation              uint64
	cancel                  context.CancelFunc
	observation             protocol.DiffObservation
	files                   []protocol.DiffFileSummary
	selectedFile            int
	wrapLines               bool
	errorText               string
	observationError        string
	annotationLayoutVersion uint64
	observationCursor       string
	scroll                  ui.ScrollPaneController
	viewportWidth           int
	cursorRevealPending     bool
	revealPendingLayout     bool
	focus                   ui.FocusNode
	appliedOpen             uint64
	resultMu                sync.Mutex
	pendingObservation      *workspaceDiffObservationResult
	pendingPoll             *workspaceDiffPollResult
	pollCancel              context.CancelFunc
	pollRequestCancel       context.CancelFunc
	pollGeneration          uint64
	polling                 bool
	changesAvailable        bool
	refreshPath             string
	refreshSide             workspaceDiffSide
	refreshLine             int
	disposed                bool
	activeTarget            protocol.DiffTargetEntry
	pendingTarget           protocol.DiffTargetEntry
	stagedObservation       protocol.DiffObservation
	catalog                 []protocol.DiffTargetEntry
	catalogLoading          bool
	catalogError            string
	catalogGeneration       uint64
	catalogCancel           context.CancelFunc
	pendingCatalog          *workspaceDiffCatalogResult
	targetPickerOpen        bool
	targetQuery             string
	targetSelection         int
	pinnedEvidence          bool
}

func (s *workspaceDiffPaneState) InitState() {
	w := s.Widget().(workspaceDiffPane)
	s.appliedOpen = w.Descriptor.OpenGeneration
	s.resetSections()
	s.active.cursorSide = workspaceDiffSideNew
	s.wrapLines = w.InitialWrapLines
	if w.Presentation.Active && !s.isFrozen(w) {
		s.startDescriptorLoad()
	}
	s.syncPolling(w)
}

func (s *workspaceDiffPaneState) DidUpdateWidget(old ui.Widget) {
	previous := old.(workspaceDiffPane)
	w := s.Widget().(workspaceDiffPane)
	if !reflect.DeepEqual(previous.Annotations, w.Annotations) {
		s.annotationLayoutVersion++
	}
	if previous.InitialWrapLines != w.InitialWrapLines && s.wrapLines != w.InitialWrapLines {
		s.wrapLines = w.InitialWrapLines
		s.invalidateDocumentLayout()
	}
	s.syncPolling(w)
	if s.isFrozen(w) {
		s.stopWork()
		s.stopCatalog()
		return
	}
	if previous.Presentation.Active && !w.Presentation.Active || previous.Presentation.Visible && !w.Presentation.Visible {
		s.stopWork()
		s.stopCatalog()
		return
	}
	if w.Descriptor.OpenGeneration != s.appliedOpen {
		s.appliedOpen = w.Descriptor.OpenGeneration
		if w.Presentation.Active {
			s.startDescriptorLoad()
		}
		return
	}
	if (!previous.Presentation.Active && w.Presentation.Active || !previous.Presentation.Visible && w.Presentation.Visible) && s.changesAvailable {
		s.applyAvailableRefresh()
		return
	}
	if (!previous.Presentation.Active || !previous.Presentation.Visible) && w.Presentation.Active && w.Presentation.Visible {
		switch {
		case s.phase == workspaceDiffInitial || s.phase == workspaceDiffLoading:
			s.startDescriptorLoad()
		case s.active.loadingFile:
			s.active.loadingFile = false
			s.active.startFileLoad()
		case s.active.loadingMore:
			s.active.loadingMore = false
			s.active.loadMoreFileDiff()
		case !s.active.highlightReady && len(s.active.hunks) > 0:
			s.active.startHighlight()
		}
	}
}

func (s *workspaceDiffPaneState) Dispose() {
	s.disposed = true
	s.stopPolling()
	for _, section := range s.sections {
		section.dispose()
	}
	s.stopCatalog()
	s.stopWork()
}

func (s *workspaceDiffPaneState) pollInterval(w workspaceDiffPane) time.Duration {
	if w.RefreshInterval > 0 {
		return w.RefreshInterval
	}
	return workspaceDiffRefreshInterval
}

func (s *workspaceDiffPaneState) pollEligible(w workspaceDiffPane) bool {
	return w.Diff != nil && w.Presentation.Active && w.Presentation.Visible && !s.isFrozen(w) && !s.pinnedEvidence &&
		s.activeTarget.Kind == protocol.DiffTargetWorkingTree && s.activeTarget.Reference != ""
}

func (s *workspaceDiffPaneState) syncPolling(w workspaceDiffPane) {
	if !s.pollEligible(w) {
		s.stopPolling()
		return
	}
	if s.pollCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.pollCancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	interval := s.pollInterval(w)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				dispatch(func() {
					if !s.disposed {
						s.SetState(func() { s.pollObservation() })
					}
				})
			}
		}
	}()
}

func (s *workspaceDiffPaneState) stopPolling() {
	if s.pollCancel != nil {
		s.pollCancel()
		s.pollCancel = nil
	}
	if s.pollRequestCancel != nil {
		s.pollRequestCancel()
		s.pollRequestCancel = nil
	}
	s.pollGeneration++
	s.polling = false
}

func (s *workspaceDiffPaneState) pollObservation() {
	w := s.Widget().(workspaceDiffPane)
	if !s.pollEligible(w) || s.polling || s.changesAvailable || s.observation.Revision == "" || s.phase == workspaceDiffLoading || s.observationCursor != "" {
		return
	}
	s.polling = true
	s.pollGeneration++
	generation := s.pollGeneration
	ctx, cancel := context.WithCancel(context.Background())
	s.pollRequestCancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	target := s.activeTarget
	go func() {
		page, err := w.Diff.ObserveDiff(ctx, protocol.ObserveDiffInput{WorkspaceID: w.Descriptor.WorkspaceID, TargetReference: target.Reference, ExpectedTargetID: target.TargetID})
		if ctx.Err() != nil {
			return
		}
		s.resultMu.Lock()
		s.pendingPoll = &workspaceDiffPollResult{generation: generation, page: page, err: err}
		s.resultMu.Unlock()
		s.dispatchPendingResults(dispatch)
	}()
}

func (s *workspaceDiffPaneState) isFrozen(w workspaceDiffPane) bool {
	return w.CurrentWorkspaceID != "" && w.CurrentWorkspaceID != w.Descriptor.WorkspaceID
}

func (s *workspaceDiffPaneState) stopCatalog() {
	if s.catalogCancel != nil {
		s.catalogCancel()
		s.catalogCancel = nil
	}
	s.catalogGeneration++
	s.catalogLoading = false
}

func (s *workspaceDiffPaneState) stopWork() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.generation++
	}
	for _, section := range s.sections {
		section.stopRead()
	}
	s.readWork.queue = nil
	s.highlightWork.queue = nil
}

func (s *workspaceDiffPaneState) startDescriptorLoad() {
	w := s.Widget().(workspaceDiffPane)
	if !w.Presentation.Active || s.isFrozen(w) {
		return
	}
	if w.Diff == nil {
		s.phase = workspaceDiffError
		s.errorText = "Repository diffs are unavailable"
		return
	}
	descriptor := w.Descriptor
	if descriptor.DiffTargetID == "" || descriptor.ExpectedRevision == "" || descriptor.Path == "" || descriptor.ExpectedFileRevision == "" {
		s.startObservation()
		return
	}
	s.stopWork()
	s.generation++
	s.activeTarget = protocol.DiffTargetEntry{}
	s.pendingTarget = protocol.DiffTargetEntry{}
	s.pinnedEvidence = true
	s.phase = workspaceDiffReady
	s.errorText = ""
	s.observation = protocol.DiffObservation{Target: protocol.DiffTarget{ID: descriptor.DiffTargetID, WorkspaceID: descriptor.WorkspaceID}, Revision: descriptor.ExpectedRevision}
	s.files = []protocol.DiffFileSummary{{Path: descriptor.Path, FileRevision: descriptor.ExpectedFileRevision, ContentState: "text"}}
	s.selectedFile = 0
	s.resetSections()
	s.active.hunks = nil
	s.active.cursorRow = 0
	s.active.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
	if descriptor.DiffSide == "old" {
		s.active.cursorSide = workspaceDiffSideOld
	} else {
		s.active.cursorSide = workspaceDiffSideNew
	}
	s.active.requestFilePage("", false)
	s.loadTargetCatalog(false)
}

func (s *workspaceDiffPaneState) startObservation() {
	w := s.Widget().(workspaceDiffPane)
	if !w.Presentation.Active || s.isFrozen(w) {
		return
	}
	if s.activeTarget.Reference == "" {
		s.loadTargetCatalog(true)
		return
	}
	if s.pollRequestCancel != nil {
		s.pollRequestCancel()
		s.pollRequestCancel = nil
		s.pollGeneration++
		s.polling = false
	}
	s.stopWork()
	if s.observation.Revision == "" {
		s.phase = workspaceDiffLoading
	}
	s.errorText = ""
	s.stagedObservation = protocol.DiffObservation{}
	s.observationError = ""
	s.observationCursor = ""
	if w.Diff == nil {
		s.phase = workspaceDiffError
		s.errorText = "Repository diffs are unavailable"
		return
	}
	s.requestObservationPage("")
}

func (s *workspaceDiffPaneState) requestObservationPage(cursor string) {
	w := s.Widget().(workspaceDiffPane)
	if w.Diff == nil || !w.Presentation.Active || s.isFrozen(w) {
		return
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.generation++
	generation := s.generation
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	target := s.activeTarget
	if s.pendingTarget.Reference != "" {
		target = s.pendingTarget
	}
	expectedRevision := ""
	if cursor != "" {
		expectedRevision = s.stagedObservation.Revision
	}
	input := protocol.ObserveDiffInput{
		WorkspaceID: w.Descriptor.WorkspaceID, TargetReference: target.Reference,
		ExpectedTargetID: target.TargetID, ExpectedTargetRevision: expectedRevision, Cursor: cursor,
	}
	go func() {
		page, err := w.Diff.ObserveDiff(ctx, input)
		if ctx.Err() != nil {
			return
		}
		s.resultMu.Lock()
		if s.pendingObservation == nil || generation >= s.pendingObservation.generation {
			s.pendingObservation = &workspaceDiffObservationResult{generation: generation, page: page, err: err}
		}
		s.resultMu.Unlock()
		s.dispatchPendingResults(dispatch)
	}()
}

func (s *workspaceDiffPaneState) completeObservation(page protocol.DiffPage, err error) {
	s.cancel = nil
	if err != nil {
		if s.observationCursor != "" {
			s.observationError = workspaceDiffErrorText(err)
		}
		message := workspaceDiffErrorText(err)
		s.pendingTarget = protocol.DiffTargetEntry{}
		if s.observation.Revision == "" {
			s.phase = workspaceDiffError
			s.errorText = message
		} else {
			if len(s.files) == 0 {
				s.phase = workspaceDiffEmpty
			} else {
				s.phase = workspaceDiffReady
			}
			s.warning(message)
			s.syncPolling(s.Widget().(workspaceDiffPane))
		}
		return
	}
	s.observationError = ""
	firstPage := s.stagedObservation.Revision == ""
	if firstPage {
		s.stagedObservation = page.Observation
		if s.refreshPath == "" && s.selectedFile >= 0 && s.selectedFile < len(s.files) {
			s.refreshPath = s.files[s.selectedFile].Path
		}
		if s.refreshPath == "" {
			s.refreshPath = s.Widget().(workspaceDiffPane).Descriptor.Path
		}
		if s.pendingTarget.Reference != "" {
			s.activeTarget = s.pendingTarget
			s.pendingTarget = protocol.DiffTargetEntry{}
			s.pinnedEvidence = false
		}
		s.observation = page.Observation
		s.files = append([]protocol.DiffFileSummary(nil), page.Files...)
		s.selectedFile = 0
		s.resetSections()
		s.active.hunks = nil
		s.selectedFile = 0
		s.active.cursorRow = 0
		s.active.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
		s.scroll = ui.ScrollPaneController{}
		s.active.invalidateSplitTargets()
		s.active.highlightReady = false
		s.phase = workspaceDiffReady
		s.active.loadingFile = false
	} else {
		s.files = append(s.files, page.Files...)
		s.ensureSections()
	}
	s.observationCursor = page.NextCursor
	if page.NextCursor != "" {
		s.requestObservationPage(page.NextCursor)
		return
	}
	s.stagedObservation = protocol.DiffObservation{}
	s.syncPolling(s.Widget().(workspaceDiffPane))
	if len(s.files) == 0 {
		s.phase = workspaceDiffEmpty
		s.active.loadingFile = false
		s.refreshPath = ""
		s.refreshLine = 0
		return
	}
	matchedPath := false
	for index, file := range s.files {
		if file.Path == s.refreshPath {
			s.selectedFile = index
			matchedPath = true
			break
		}
	}
	if !matchedPath {
		s.refreshPath = ""
		s.refreshLine = 0
	}
	s.ensureSections()
	if !s.active.loaded && !s.active.loadingFile {
		s.active.startFileLoad()
	}
}

func (s *workspaceDiffPaneState) completePoll(page protocol.WorkingTreePage, err error) {
	s.polling = false
	s.pollRequestCancel = nil
	if !s.pollEligible(s.Widget().(workspaceDiffPane)) {
		return
	}
	if err != nil {
		s.loadTargetCatalog(false)
		return
	}
	if page.Observation.Revision == "" || page.Observation.Revision == s.observation.Revision {
		return
	}
	s.changesAvailable = true
	s.applyAvailableRefresh()
}

func (s *workspaceDiffPaneState) refreshBlocked() bool {
	return s.active.commenting || s.active.commentPending || s.active.commentLoading || s.active.selectionActive()
}

func (s *workspaceDiffPaneState) applyAvailableRefresh() {
	if !s.changesAvailable || s.refreshBlocked() {
		return
	}
	w := s.Widget().(workspaceDiffPane)
	if !w.Presentation.Active || !w.Presentation.Visible || s.isFrozen(w) {
		return
	}
	if s.selectedFile >= 0 && s.selectedFile < len(s.files) {
		s.refreshPath = s.files[s.selectedFile].Path
	}
	if line, ok := s.active.lineAtRow(s.active.cursorRow); ok {
		coordinate := line.NewLine
		if s.active.cursorSide == workspaceDiffSideOld {
			coordinate = line.OldLine
		}
		if coordinate != nil {
			s.refreshSide = s.active.cursorSide
			s.refreshLine = *coordinate
		}
	}
	s.changesAvailable = false
	s.startObservation()
}

func (s *workspaceDiffPaneState) warning(message string) {
	if callback := s.Widget().(workspaceDiffPane).OnWarning; callback != nil {
		callback(message)
	}
}

func (s *workspaceDiffPaneState) loadTargetCatalog(selectInitial bool) {
	w := s.Widget().(workspaceDiffPane)
	if w.Diff == nil || s.catalogLoading || !w.Presentation.Active || s.isFrozen(w) {
		return
	}
	if s.catalogCancel != nil {
		s.catalogCancel()
	}
	s.catalogGeneration++
	generation := s.catalogGeneration
	ctx, cancel := context.WithCancel(context.Background())
	s.catalogCancel = cancel
	s.catalogLoading = true
	s.catalogError = ""
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	go func() {
		catalog, err := w.Diff.ListDiffTargets(ctx, protocol.ListDiffTargetsInput{WorkspaceID: w.Descriptor.WorkspaceID})
		if ctx.Err() != nil {
			return
		}
		s.resultMu.Lock()
		s.pendingCatalog = &workspaceDiffCatalogResult{generation: generation, catalog: catalog, err: err}
		s.resultMu.Unlock()
		s.dispatchPendingResults(dispatch)
	}()
	_ = selectInitial // completion always initializes a target when needed.
}

func (s *workspaceDiffPaneState) completeCatalog(catalog protocol.DiffTargetCatalog, err error) {
	s.catalogLoading = false
	s.catalogCancel = nil
	if err != nil {
		s.catalogError = workspaceDiffErrorText(err)
		if s.observation.Revision == "" {
			s.phase = workspaceDiffError
			s.errorText = "Could not list diff targets"
		}
		return
	}
	s.catalog = append([]protocol.DiffTargetEntry(nil), catalog.Targets...)
	s.catalogError = ""
	if len(s.catalog) == 0 && s.observation.Revision == "" {
		s.phase = workspaceDiffError
		s.errorText = "No diff targets available"
	}
	wantedTargetID := s.observation.Target.ID
	if wantedTargetID == "" {
		wantedTargetID = s.activeTarget.TargetID
	}
	matchedTarget := false
	if wantedTargetID != "" {
		for _, target := range s.catalog {
			if target.TargetID == wantedTargetID {
				s.activeTarget = target
				matchedTarget = true
				break
			}
		}
	}
	if wantedTargetID != "" && !matchedTarget && s.activeTarget.Kind == protocol.DiffTargetWorkingTree && len(s.catalog) > 0 && s.catalog[0].Kind == protocol.DiffTargetWorkingTree {
		if s.refreshBlocked() {
			s.pendingTarget = s.catalog[0]
			s.changesAvailable = true
		} else {
			s.switchTarget(s.catalog[0])
		}
	}
	if s.activeTarget.Reference == "" && s.observation.Target.ID == "" && len(s.catalog) > 0 {
		s.activeTarget = s.catalog[0]
		s.startObservation()
	}
	s.ensureTargetSelection()
}

func (s *workspaceDiffPaneState) switchTarget(target protocol.DiffTargetEntry) {
	if s.refreshBlocked() {
		s.warning("Finish the active range or comment before changing target")
		return
	}
	if target.Reference == "" {
		return
	}
	s.targetPickerOpen = false
	s.targetQuery = ""
	if target.TargetID == s.activeTarget.TargetID && !s.pinnedEvidence {
		return
	}
	w := s.Widget().(workspaceDiffPane)
	if target.Kind != protocol.DiffTargetWorkingTree && s.activeTarget.Kind == protocol.DiffTargetWorkingTree &&
		(len(s.files) > 0 || s.observation.IndexSummary != "" && s.observation.IndexSummary != "clean") && w.OnNotice != nil {
		w.OnNotice("Working-tree changes are not included in this committed target")
	}
	if s.selectedFile >= 0 && s.selectedFile < len(s.files) {
		s.refreshPath = s.files[s.selectedFile].Path
	}
	s.refreshLine = 0
	s.pendingTarget = target
	s.phase = workspaceDiffLoading
	s.active.loadingFile = false
	s.stopPolling()
	s.startObservation()
}

func (s *workspaceDiffPaneState) openTargetPicker() {
	if s.refreshBlocked() {
		s.warning("Finish the active range or comment before changing target")
		return
	}
	s.targetPickerOpen = true
	s.targetQuery = ""
	s.ensureTargetSelection()
	s.loadTargetCatalog(false)
}

func (s *workspaceDiffPaneState) toggleTarget() {
	if s.refreshBlocked() {
		s.warning("Finish the active range or comment before changing target")
		return
	}
	if len(s.catalog) == 0 {
		s.loadTargetCatalog(false)
		s.warning("Diff targets are still loading")
		return
	}
	working := s.catalog[0]
	if s.activeTarget.Kind != protocol.DiffTargetWorkingTree {
		s.switchTarget(working)
		return
	}
	for _, target := range s.catalog {
		if target.Kind == protocol.DiffTargetCommit && target.Head.OID != "" && target.Head.OID == working.Head.OID {
			s.switchTarget(target)
			return
		}
	}
	s.warning("The current HEAD commit is not available")
}

func (s *workspaceDiffPaneState) filteredTargets() []protocol.DiffTargetEntry {
	query := strings.ToLower(strings.TrimSpace(s.targetQuery))
	if query == "" {
		return append([]protocol.DiffTargetEntry(nil), s.catalog...)
	}
	result := make([]protocol.DiffTargetEntry, 0, len(s.catalog))
	for _, target := range s.catalog {
		haystack := strings.ToLower(strings.Join([]string{target.Metadata.Label, target.Metadata.Subject, target.Metadata.RefName, target.Metadata.BaseRefName, target.Metadata.Abbreviated}, " "))
		if strings.Contains(haystack, query) {
			result = append(result, target)
		}
	}
	return result
}

func (s *workspaceDiffPaneState) ensureTargetSelection() {
	targets := s.filteredTargets()
	if len(targets) == 0 {
		s.targetSelection = 0
		return
	}
	if strings.TrimSpace(s.targetQuery) == "" {
		for index, target := range targets {
			if target.TargetID == s.activeTarget.TargetID {
				s.targetSelection = index
				return
			}
		}
	}
	s.targetSelection = min(max(0, s.targetSelection), len(targets)-1)
}

func workspaceDiffErrorText(err error) string {
	var diffErr *protocol.DiffError
	if errors.As(err, &diffErr) && strings.TrimSpace(diffErr.Message) != "" {
		return diffErr.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The diff observation timed out"
	}
	return "Could not load diff"
}

func workspaceDiffHighlightSources(hunks []protocol.DiffHunk) (string, string) {
	var oldSource, newSource strings.Builder
	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			if line.OldLine != nil {
				oldSource.WriteString(line.Content)
				oldSource.WriteByte('\n')
			}
			if line.NewLine != nil {
				newSource.WriteString(line.Content)
				newSource.WriteByte('\n')
			}
		}
	}
	return oldSource.String(), newSource.String()
}

func workspaceDiffSplitWidths(viewport int) (int, int) {
	available := max(2, viewport-1)
	oldWidth := available / 2
	return oldWidth, available - oldWidth
}

func workspaceDiffSideGutterWidth(line protocol.DiffLine, newSide bool) int {
	number, marker := "", " "
	if newSide && line.NewLine != nil {
		number = fmt.Sprintf("%d", *line.NewLine)
	} else if !newSide && line.OldLine != nil {
		number = fmt.Sprintf("%d", *line.OldLine)
	}
	if line.Kind == "addition" {
		marker = "+"
	} else if line.Kind == "deletion" {
		marker = "−"
	}
	return uucode.StringWidth(fmt.Sprintf("%5s %s ", number, marker))
}

func workspaceDiffWrappedLineHeight(line protocol.DiffLine, newSide bool, sideWidth int) int {
	contentWidth := max(1, sideWidth-workspaceDiffSideGutterWidth(line, newSide))
	layout := ui.LayoutText(
		[]ui.TextSpan{{Text: highlight.Sanitize(line.Content)}},
		ui.Constraints{MaxWidth: contentWidth, MaxHeight: ui.Unbounded},
		ui.TextLayoutOptions{SoftWrap: true},
	)
	return max(1, layout.Size.Height)
}

func workspaceDiffSplitPairHeight(oldLine, newLine *protocol.DiffLine, viewport int, wrap bool) int {
	if !wrap {
		return 1
	}
	oldWidth, newWidth := workspaceDiffSplitWidths(viewport)
	height := 1
	if oldLine != nil {
		height = max(height, workspaceDiffWrappedLineHeight(*oldLine, false, oldWidth))
	}
	if newLine != nil {
		height = max(height, workspaceDiffWrappedLineHeight(*newLine, true, newWidth))
	}
	return height
}

func (s *workspaceDiffPaneState) TickFrame(time.Time) bool {
	w := s.Widget().(workspaceDiffPane)
	if !w.Presentation.Active || !w.Presentation.Visible {
		return false
	}
	needsFrame := false
	if s.scrollCorrection != 0 && !s.cursorRevealPending {
		vertical := s.scroll.Metrics(ui.ScrollVertical)
		s.scroll.ScrollTo(s.horizontalOffset(), max(0, vertical.ScrollOffset+s.scrollCorrection))
	}
	s.scrollCorrection = 0
	if s.scroll.Attached() {
		offset := s.scroll.Metrics(ui.ScrollVertical).ScrollOffset
		if offset != s.lastViewportOffset {
			s.lastViewportOffset = offset
			s.MarkNeedsBuild()
			needsFrame = true
		}
	}
	if s.cursorRevealPending {
		if s.revealPendingLayout {
			s.revealPendingLayout = false
			return true
		}
		s.cursorRevealPending = false
		needsFrame = true
		if s.jumpToFile {
			s.scroll.ScrollTo(s.horizontalOffset(), s.active.offset)
			s.jumpToFile = false
		} else {
			s.active.revealCursor()
		}
	}
	if s.loadVisibleSections() {
		s.MarkNeedsBuild()
		needsFrame = true
	}
	return needsFrame
}

func (s *workspaceDiffPaneState) horizontalOffset() int {
	if s.splitLayout() || !s.scroll.Attached() {
		return 0
	}
	return s.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset
}

func (s *workspaceDiffPaneState) moveFile(delta int) {
	if len(s.files) == 0 || delta == 0 {
		return
	}
	if s.active.selectionActive() {
		s.active.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
		if s.changesAvailable {
			s.applyAvailableRefresh()
			return
		}
	}
	next := (s.selectedFile + delta) % len(s.files)
	if next < 0 {
		next += len(s.files)
	}
	if !s.activateSection(next) {
		return
	}
	if !s.active.loaded && !s.active.loadingFile {
		s.active.startFileLoad()
	}
	s.jumpToFile = true
	s.cursorRevealPending = true
	s.revealPendingLayout = true
}

func (s *workspaceDiffPaneState) dispatchPendingResults(dispatch func(func())) {
	dispatch(func() {
		if !s.disposed {
			s.SetState(s.applyPendingResults)
		}
	})
}

func (s *workspaceDiffPaneState) applyPendingResults() {
	s.resultMu.Lock()
	observation := s.pendingObservation
	poll := s.pendingPoll
	catalog := s.pendingCatalog
	s.pendingObservation = nil
	s.pendingPoll = nil
	s.pendingCatalog = nil
	s.resultMu.Unlock()
	if observation != nil && observation.generation == s.generation {
		s.completeObservation(observation.page, observation.err)
	}
	if poll != nil && poll.generation == s.pollGeneration {
		s.completePoll(poll.page, poll.err)
	}
	if catalog != nil && catalog.generation == s.catalogGeneration {
		s.completeCatalog(catalog.catalog, catalog.err)
	}
}

func (s *workspaceDiffPaneState) targetLabel() string {
	target := s.activeTarget
	if s.pendingTarget.TargetID != "" {
		target = s.pendingTarget
	}
	if target.Metadata.Label != "" {
		return target.Metadata.Label
	}
	switch target.Kind {
	case protocol.DiffTargetWorkingTree:
		return "Working tree"
	case protocol.DiffTargetCommit:
		return strings.TrimSpace(target.Metadata.Abbreviated + "  " + target.Metadata.Subject)
	case protocol.DiffTargetBranch:
		if target.Metadata.RefName != "" {
			return target.Metadata.RefName + " vs " + target.Metadata.BaseRefName
		}
	}
	if s.observation.Target.ID != "" {
		shortOID := func(oid string) string {
			if len(oid) > 7 {
				return oid[:7]
			}
			return oid
		}
		switch s.observation.Target.Kind {
		case protocol.DiffTargetCommit:
			if oid := shortOID(s.observation.Target.Head.OID); oid != "" {
				return oid + "  Commit"
			}
		case protocol.DiffTargetBranch:
			if head, base := shortOID(s.observation.Target.Head.OID), shortOID(s.observation.Target.Base.OID); head != "" && base != "" {
				return head + " vs " + base
			}
		}
		id := strings.TrimPrefix(s.observation.Target.ID, "difftarget_")
		if len(id) > 8 {
			id = id[:8]
		}
		return id
	}
	return "Working tree"
}

func (s *workspaceDiffPaneState) targetPicker(ctx ui.BuildContext, theme ui.Theme) ui.Widget {
	targets := s.filteredTargets()
	presentation := resolvePickerRowPresentation(ctx, theme)
	rows := make([]ui.Widget, 0, max(1, len(targets)))
	for index, target := range targets {
		index, target := index, target
		selected := index == s.targetSelection
		foreground, background := presentation.ItemText, theme.Background
		rowTheme := presentation.Theme
		if selected {
			foreground, background = presentation.FocusedText, presentation.FocusedBg
		}
		marker := "  "
		if target.TargetID == s.activeTarget.TargetID {
			marker = glyphCheck + " "
		}
		draftCount := 0
		if target.AnnotationCount != nil {
			draftCount = *target.AnnotationCount
		} else {
			for _, annotation := range s.Widget().(workspaceDiffPane).Annotations {
				if anchor := annotation.Anchor.WorkingTreeDiff; anchor != nil && anchor.TargetID == target.TargetID {
					draftCount++
				}
			}
		}
		draft := ""
		if draftCount > 0 {
			draft = fmt.Sprintf("  %s %d", glyphCircleFilled, draftCount)
		}
		label := target.Metadata.Label
		if label == "" {
			label = target.Metadata.Abbreviated + "  " + target.Metadata.Subject
		}
		rows = append(rows, ui.Provider[ui.Theme]{Value: rowTheme, Child: ui.ListTile{Selected: selected, MinHeight: 1, Padding: ui.Insets{Left: 1, Right: 1}, OnPressed: func(ui.EventContext) {
			s.SetState(func() { s.targetSelection = index; s.switchTarget(target) })
		}, Title: ui.Text{Value: marker + label + draft, Style: ui.Style{Foreground: foreground, Background: background}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}}})
	}
	if s.catalogLoading && len(rows) == 0 {
		rows = append(rows, ui.Center(spinnerWithLabel("Loading diff targets…", ui.Style{Foreground: theme.MutedForeground})))
	} else if s.catalogError != "" {
		rows = append(rows, ui.Text{Value: glyphCross + " " + s.catalogError, Style: ui.Style{Foreground: theme.DangerText}, MaxLines: 2})
	} else if len(rows) == 0 {
		rows = append(rows, ui.Center(ui.Text{Value: "No matching targets", Style: ui.Style{Foreground: theme.MutedForeground}}))
	}
	cursor := len(s.targetQuery)
	fieldTheme := theme
	fieldTheme.Surface, fieldTheme.SurfaceHovered = theme.Background, theme.Background
	query := ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{ui.Text{Value: ">"}, ui.SizedBox{Width: 1}, textInput(fieldTheme, textInputConfig{Value: s.targetQuery, Placeholder: "Filter branch, subject, or object ID…", CursorOffset: &cursor, AutoFocus: true, OnChanged: func(_ ui.EventContext, value string) {
		s.SetState(func() { s.targetQuery = value; s.targetSelection = 0 })
	}, OnSubmitted: func(ui.EventContext, string) {
		filtered := s.filteredTargets()
		if len(filtered) > 0 {
			s.SetState(func() { s.switchTarget(filtered[s.targetSelection]) })
		}
	}})}}
	body := ui.Padding(ui.Insets{Top: 1, Left: 2, Right: 2}, ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Text{Value: "Select diff target", Style: ui.Style{Foreground: theme.Foreground}}, ui.SizedBox{Height: 1}, query, ui.SizedBox{Height: 1}, ui.Expanded(ui.ScrollView{Child: ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}}),
	}})
	content := pickerDialogContent(theme, body, ui.Text{Value: "↑↓ move · enter select · esc close", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	actions := map[ui.IntentType]ui.ActionFunc{
		moveWorkspaceDiffTargetIntent{}.IntentType(): func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			s.SetState(func() {
				targets := s.filteredTargets()
				if len(targets) > 0 {
					s.targetSelection = (s.targetSelection + intent.(moveWorkspaceDiffTargetIntent).delta + len(targets)) % len(targets)
				}
			})
			return ui.EventHandled
		},
		ui.DismissIntentType: func(ui.EventContext, ui.Intent) ui.EventResult {
			s.SetState(func() { s.targetPickerOpen = false; s.targetQuery = "" })
			return ui.EventHandled
		},
	}
	content = ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: ui.ShortcutMap{"Up": moveWorkspaceDiffTargetIntent{delta: -1}, "Down": moveWorkspaceDiffTargetIntent{delta: 1}}, Child: content}}
	return pickerDialogPositioner{Percent: 70, MinWidth: 48, MaxWidth: 96, Height: pickerModalMinHeight, Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: content}}
}

func (s *workspaceDiffPaneState) Build(ctx ui.BuildContext) ui.Widget {
	w := s.Widget().(workspaceDiffPane)
	theme := ui.MustDepend[ui.Theme](ctx)
	semantic, ok := ui.Depend[SemanticTheme](ctx)
	if !ok {
		semantic = semanticFallback(theme)
	}
	summary, target := "No changed files", s.targetLabel()
	if s.pendingTarget.Reference != "" {
		summary = "Loading files…"
		target = "Switching target…  " + glyphMiddleDot + "  " + target
	} else if len(s.files) > 0 && s.selectedFile >= 0 && s.selectedFile < len(s.files) {
		summary = fmt.Sprintf("%d changed files", len(s.files))
		if len(s.files) == 1 {
			summary = "1 changed file"
		}
	}
	if s.isFrozen(w) {
		target = "Frozen  " + glyphMiddleDot + "  " + target
	}
	revision := ui.Widget(ui.Text{Value: target, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis})
	if w.Presentation.Active {
		revision = headerControl{Label: target, OnPressed: func(event ui.EventContext) {
			if w.OnFocusRequest != nil {
				w.OnFocusRequest(event)
			}
			s.SetState(func() { s.openTargetPicker() })
		}}
	}
	wrapLabel := "Wrap off"
	if s.wrapLines {
		wrapLabel = "Wrap on"
	}
	wrapControl := headerControl{Label: wrapLabel, Primary: s.wrapLines, OnPressed: func(ui.EventContext) { s.toggleWrapLines() }}
	layoutLabel := "Unified"
	if s.splitLayout() {
		layoutLabel = "Split"
	}
	layoutControl := ui.Widget(headerControl{Label: layoutLabel, OnPressed: func(ui.EventContext) {
		s.SetState(func() { s.unifiedLayout = !s.unifiedLayout; s.invalidateDocumentLayout() })
	}})
	if s.viewportWidth < workspaceDiffSplitBreakpoint {
		layoutControl = ui.Text{Value: layoutLabel, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1}
	}

	header := ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			ui.Expanded(ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
				ui.Flexible(revision),
				ui.Text{Value: "  " + summary, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
			}}),
			wrapControl,
			ui.Text{Value: "  "},
			layoutControl,
		}}),
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
	}}
	body := s.body(theme, semantic)
	footer := workspacePanelFooter(theme, s.footerText())
	bindings := map[ui.IntentType]ui.ActionFunc{}
	shortcuts := ui.ShortcutMap{}
	if w.Presentation.Active && !s.targetPickerOpen {
		bindings[toggleWorkspaceDiffTargetIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
			s.SetState(func() { s.toggleTarget() })
			return ui.EventHandled
		}
		bindings[openWorkspaceDiffTargetPickerIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
			s.SetState(func() { s.openTargetPicker() })
			return ui.EventHandled
		}
		if !s.active.commenting {
			shortcuts["g"] = toggleWorkspaceDiffTargetIntent{}
			shortcuts["Shift+g"] = openWorkspaceDiffTargetPickerIntent{}
		}
		if s.active.commenting {
			bindings[ui.IntentType("vaxis.dismiss")] = func(ui.EventContext, ui.Intent) ui.EventResult {
				if !s.active.commentPending {
					s.SetState(func() { s.active.closeComment() })
				}
				return ui.EventHandled
			}
		} else if w.OnCreateAnnotation != nil {
			bindings[selectWorkspaceDiffRangeIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
				s.SetState(func() {
					if s.active.selectionActive() {
						s.active.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
						s.applyAvailableRefresh()
					} else if anchor, ok := s.active.currentDiffAnchor(); ok {
						s.active.selectionAnchor = anchor
					}
				})
				return ui.EventHandled
			}
			shortcuts["v"] = selectWorkspaceDiffRangeIntent{}
			bindings[commentWorkspaceDiffIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
				s.active.beginComment(w)
				return ui.EventHandled
			}
			shortcuts["c"] = commentWorkspaceDiffIntent{}
			if annotation, ok := s.active.annotationAtCursor(w); ok {
				if w.OnLoadAnnotation != nil {
					bindings[editWorkspaceDiffAnnotationIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
						s.active.beginEditComment(w, annotation)
						return ui.EventHandled
					}
					shortcuts["e"] = editWorkspaceDiffAnnotationIntent{}
				}
				if w.OnRemoveAnnotation != nil {
					bindings[removeWorkspaceDiffAnnotationIntent{}.IntentType()] = func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
						w.OnRemoveAnnotation(ctx, annotation.ID)
						return ui.EventHandled
					}
					shortcuts["d"] = removeWorkspaceDiffAnnotationIntent{}
				}
			}
		}
		if !s.active.commenting {
			bindings[moveWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
				movement := intent.(moveWorkspaceDiffIntent)
				s.SetState(func() {
					if movement.lines != 0 {
						s.moveLine(movement.lines)
					}
					if movement.columns != 0 {
						if s.splitLayout() {
							s.active.panSplit(movement.columns)
						} else {
							s.scroll.ScrollBy(movement.columns, 0)
						}
					}
				})
				return ui.EventHandled
			}
			bindings[moveWorkspaceDiffHunkIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
				s.SetState(func() { s.moveHunk(intent.(moveWorkspaceDiffHunkIntent).delta) })
				return ui.EventHandled
			}
			shortcuts["Up"] = moveWorkspaceDiffIntent{lines: -1}
			shortcuts["k"] = moveWorkspaceDiffIntent{lines: -1}
			shortcuts["Down"] = moveWorkspaceDiffIntent{lines: 1}
			shortcuts["j"] = moveWorkspaceDiffIntent{lines: 1}
			shortcuts["Left"] = moveWorkspaceDiffIntent{columns: -4}
			shortcuts["h"] = moveWorkspaceDiffIntent{columns: -4}
			shortcuts["Right"] = moveWorkspaceDiffIntent{columns: 4}
			shortcuts["l"] = moveWorkspaceDiffIntent{columns: 4}
			shortcuts["{"] = moveWorkspaceDiffHunkIntent{delta: -1}
			shortcuts["}"] = moveWorkspaceDiffHunkIntent{delta: 1}
			bindings[toggleWorkspaceDiffWrapIntent{}.IntentType()] = func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
				s.toggleWrapLines()
				return ui.EventHandled
			}
			bindings[toggleWorkspaceDiffLayoutIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
				if s.viewportWidth < workspaceDiffSplitBreakpoint {
					s.warning("Split diff requires at least 120 columns")
					return ui.EventHandled
				}
				s.SetState(func() { s.unifiedLayout = !s.unifiedLayout; s.invalidateDocumentLayout() })
				return ui.EventHandled
			}
			shortcuts["s"] = toggleWorkspaceDiffLayoutIntent{}

			bindings[panWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
				s.SetState(func() { s.active.panSplit(intent.(panWorkspaceDiffIntent).columns) })
				return ui.EventHandled
			}
			shortcuts["w"] = toggleWorkspaceDiffWrapIntent{}
			shortcuts["H"] = panWorkspaceDiffIntent{columns: -4}
			shortcuts["L"] = panWorkspaceDiffIntent{columns: 4}
			if !s.isFrozen(w) {
				bindings[moveWorkspaceDiffFileIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
					s.SetState(func() { s.moveFile(intent.(moveWorkspaceDiffFileIntent).delta) })
					return ui.EventHandled
				}
				bindings[refreshWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
					s.SetState(func() {
						if s.refreshBlocked() {
							s.changesAvailable = true
							s.warning("Clear the selection before refreshing the diff")
							return
						}
						s.changesAvailable = true
						s.applyAvailableRefresh()
					})
					return ui.EventHandled
				}
				shortcuts["["] = moveWorkspaceDiffFileIntent{delta: -1}
				shortcuts["]"] = moveWorkspaceDiffFileIntent{delta: 1}
				shortcuts["r"] = refreshWorkspaceDiffIntent{}
				if s.active.fileNextCursor != "" && !s.active.loadingMore {
					bindings[loadMoreWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
						s.SetState(func() { s.active.loadMoreFileDiff() })
						return ui.EventHandled
					}
					shortcuts["m"] = loadMoreWorkspaceDiffIntent{}
				}
			}
		}
	}
	content := ui.Widget(workspacePanelLayout{Header: header, Body: body, Footer: footer})
	if s.targetPickerOpen {
		content = ui.Overlay{Child: content, Entries: []ui.OverlayEntry{{Modal: true, Barrier: clearModalBarrier{}, Child: s.targetPicker(ctx, theme)}}}
	}
	content = ui.Focus(&s.focus, content)
	content = ui.FocusScope{AutoFocus: w.Presentation.Active, Child: content}
	content = mouseReleaseListener{Child: content, OnRelease: func(ui.EventContext) { s.active.finishGutterRange(w) }}
	content = mouseActivator{Child: content, DefaultMouseShape: true, OnScroll: func(_ ui.EventContext, mouse ui.Mouse) ui.EventResult {
		if !s.splitLayout() || s.wrapLines {
			return ui.EventIgnored
		}
		delta := 0
		switch mouse.Button {
		case ui.MouseWheelLeft:
			delta = -4
		case ui.MouseWheelRight:
			delta = 4
		case ui.MouseWheelUp:
			if mouse.Modifiers&vaxis.ModShift != 0 {
				delta = -4
			}
		case ui.MouseWheelDown:
			if mouse.Modifiers&vaxis.ModShift != 0 {
				delta = 4
			}
		}
		if delta == 0 {
			return ui.EventIgnored
		}
		s.SetState(func() { s.active.panSplit(delta) })
		return ui.EventHandled
	}, OnPrimaryDownCapture: func(event ui.EventContext) {
		if w.OnFocusRequest != nil {
			w.OnFocusRequest(event)
		}
		s.focus.RequestFocus()
	}}
	return ui.Actions{Bindings: bindings, Child: keyShortcuts{Bindings: shortcuts, Child: content}}
}

func (s *workspaceDiffPaneState) body(theme ui.Theme, semantic SemanticTheme) ui.Widget {
	var child ui.Widget
	switch {
	case s.phase == workspaceDiffLoading:
		child = centeredWorkspaceDiffLoading(s.viewportWidth, "Loading diff…", ui.Style{Foreground: theme.MutedForeground})
	case s.phase == workspaceDiffEmpty:
		empty := "No changes for " + s.targetLabel()
		child = ui.Center(ui.Text{Value: empty, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	case s.phase == workspaceDiffError:
		child = ui.Center(ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			ui.Text{Value: "Could not load diff", Style: ui.Style{Foreground: theme.DangerText}, MaxLines: 1},
			ui.Text{Value: s.errorText, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
		}})
	default:
		child = s.documentBody(theme, semantic)
	}

	content := ui.Widget(ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{ui.Expanded(child)}})
	return widthProbe{WidthChanged: func(width int) {
		if width != s.viewportWidth {
			s.viewportWidth = width
			s.invalidateDocumentLayout()
			s.MarkNeedsBuild()
		}
	}, Child: content}
}

func centeredWorkspaceDiffLoading(viewportWidth int, label string, style ui.Style) ui.Widget {
	loadingWidth := len([]rune(spinnerFrames[0] + " " + label))
	loading := ui.Stack{Children: []ui.Widget{
		ui.Text{Value: strings.Repeat(" ", max(1, viewportWidth)), MaxLines: 1},
		ui.Positioned{Left: max(0, (viewportWidth-loadingWidth)/2), Child: spinner{Style: style, Label: label}},
	}}
	return ui.Flex{Axis: ui.Vertical, MainAxisAlignment: ui.MainAxisCenter, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{loading}}
}

func workspaceDiffContentStateText(file protocol.DiffFileSummary) string {
	switch file.ContentState {
	case "binary":
		return "Binary file changed"
	case "conflict":
		return "File has unresolved conflicts"
	case "intent_to_add":
		return "Intent-to-add file has no textual diff"
	case "unsupported_transform":
		return "File uses an unsupported content transform"
	case "unsupported_kind":
		return "File kind is not available for textual diff"
	case "too_large":
		return "File diff exceeds the configured limit"
	case "unavailable":
		return "File diff is unavailable"
	default:
		return "No textual changes"
	}
}

func workspaceDiffUnifiedLineWidth(line protocol.DiffLine) int {
	oldNumber, newNumber, marker := "", "", " "
	if line.OldLine != nil {
		oldNumber = fmt.Sprintf("%d", *line.OldLine)
	}
	if line.NewLine != nil {
		newNumber = fmt.Sprintf("%d", *line.NewLine)
	}
	if line.Kind == "addition" {
		marker = "+"
	} else if line.Kind == "deletion" {
		marker = "−"
	}
	gutter := fmt.Sprintf("%5s %5s %s ", oldNumber, newNumber, marker)
	return uucode.StringWidth(gutter) + uucode.StringWidth(highlight.Sanitize(line.Content))
}

func workspaceDiffWidgetHeight(row ui.Widget) int {
	if box, ok := row.(ui.SizedBox); ok {
		return max(1, box.Height)
	}
	return 1
}

func workspaceDiffWidgetsHeight(rows []ui.Widget) int {
	height := 0
	for _, row := range rows {
		height += workspaceDiffWidgetHeight(row)
	}
	return height
}

func workspaceDiffSplitSupplementRows(rows []ui.Widget, oldSide bool, viewportWidth int, theme ui.Theme) []ui.Widget {
	oldWidth, newWidth := workspaceDiffSplitWidths(viewportWidth)
	wrapped := make([]ui.Widget, 0, len(rows))
	for _, row := range rows {
		height := workspaceDiffWidgetHeight(row)
		oldChild := ui.Widget(ui.SizedBox{Width: oldWidth, Height: height})
		newChild := ui.Widget(ui.SizedBox{Width: newWidth, Height: height})
		if oldSide {
			oldChild = ui.SizedBox{Width: oldWidth, Height: height, Child: row}
		} else {
			newChild = ui.SizedBox{Width: newWidth, Height: height, Child: row}
		}
		wrapped = append(wrapped, ui.SizedBox{Width: oldWidth + newWidth + 1, Height: height, Child: ui.Flex{
			Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, CrossAxisAlignment: ui.CrossAxisStretch,
			Children: []ui.Widget{
				oldChild,
				ui.Text{Value: strings.TrimSuffix(strings.Repeat(glyphTableSeparator+"\n", height), "\n"), Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: height},
				newChild,
			},
		}})
	}
	return wrapped
}

func workspaceDiffSliceSpans(spans []ui.TextSpan, columns int) []ui.TextSpan {
	if columns <= 0 {
		return spans
	}
	result := make([]ui.TextSpan, 0, len(spans))
	remaining := columns
	for _, span := range spans {
		if remaining <= 0 {
			result = append(result, span)
			continue
		}
		iterator := uucode.NewGraphemeWidthIterator(span.Text)
		start := len(span.Text)
		for {
			grapheme, ok := iterator.Next()
			if !ok {
				break
			}
			if remaining < grapheme.Width {
				start = grapheme.End
				remaining = 0
				break
			}
			remaining -= grapheme.Width
			start = grapheme.End
			if remaining == 0 {
				break
			}
		}
		if start < len(span.Text) {
			span.Text = span.Text[start:]
			result = append(result, span)
		}
	}
	return result
}

func workspaceDiffGutter(base ui.Widget, width int, selected bool, theme ui.Theme, onPressed ui.VoidCallback, onMotion func(ui.EventContext, ui.Mouse)) ui.Widget {
	children := []ui.Widget{base}
	if selected {
		button := ui.SizedBox{Width: 3, Height: 1, Child: ui.Text{
			Value: " + ", Style: ui.Style{Foreground: theme.Background, Background: theme.Primary}, MaxLines: 1,
		}}
		children = append(children, ui.Positioned{Left: width - 3, Top: 0, Child: button})
	}
	return mouseActivator{Child: ui.SizedBox{Width: width, Height: 1, Child: ui.Stack{Children: children}}, OnPressed: onPressed, OnMotion: onMotion}
}

func workspaceDiffSideWidget(item *workspaceDiffRenderedLine, selected, rangeSelected, actionSide bool, height int, wrap bool, columnOffset int, theme ui.Theme, semantic SemanticTheme, moveCursor, gutterPress ui.VoidCallback, gutterMotion func(ui.EventContext, ui.Mouse)) ui.Widget {
	if item == nil {
		return ui.SizedBox{Height: height, Child: ui.Text{Value: "", Style: ui.Style{Background: theme.Background}, MaxLines: 1}}
	}
	line := item.line
	number, marker := "", " "
	if actionSide && line.NewLine != nil {
		number = fmt.Sprintf("%d", *line.NewLine)
	} else if !actionSide && line.OldLine != nil {
		number = fmt.Sprintf("%d", *line.OldLine)
	}
	contentBackground, gutterBackground := theme.Background, theme.Background
	switch line.Kind {
	case "addition":
		marker = "+"
		contentBackground = semantic.Token(kittheme.TokenDiffAddedContentBackground)
		gutterBackground = semantic.Token(kittheme.TokenDiffAddedLineNumberBg)
	case "deletion":
		marker = "−"
		contentBackground = semantic.Token(kittheme.TokenDiffRemovedContentBg)
		gutterBackground = semantic.Token(kittheme.TokenDiffRemovedLineNumberBg)
	}
	gutterForeground := theme.MutedForeground
	if line.Kind == "addition" || line.Kind == "deletion" {
		gutterForeground = theme.Foreground
	}
	gutterStyle := ui.Style{Foreground: gutterForeground, Background: gutterBackground}
	if rangeSelected {
		gutterStyle.Foreground = theme.Foreground
		gutterStyle.Attribute = ui.AttrBold
	}
	spans := append([]ui.TextSpan(nil), item.syntax...)
	if len(spans) == 0 {
		spans = []ui.TextSpan{{Text: highlight.Sanitize(line.Content), Style: ui.Style{Foreground: theme.Foreground, Background: contentBackground}}}
	} else {
		for index := range spans {
			spans[index].Style.Background = contentBackground
		}
	}
	if !wrap && columnOffset > 0 {
		spans = workspaceDiffSliceSpans(spans, columnOffset)
	}
	gutter := workspaceDiffGutter(ui.RichText{Spans: []ui.TextSpan{{Text: fmt.Sprintf("%5s ", number), Style: gutterStyle}, {Text: marker + " ", Style: gutterStyle}}, SoftWrap: false}, 8, selected, theme, gutterPress, gutterMotion)
	content := ui.SizedBox{Height: height, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: contentBackground}},
		ui.RichText{Spans: spans, SoftWrap: wrap},
	)}
	row := ui.Widget(ui.Flex{Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, CrossAxisAlignment: ui.CrossAxisStart, Children: []ui.Widget{
		gutter,
		ui.Expanded(content),
	}})
	return mouseActivator{Child: row, OnHover: moveCursor, OnPressed: moveCursor}
}

func workspaceDiffSyntaxLines(result highlight.Result, semantic SemanticTheme) [][]ui.TextSpan {
	lines := strings.Split(result.Source, "\n")
	if strings.HasSuffix(result.Source, "\n") {
		lines = lines[:len(lines)-1]
	}
	rows := make([][]ui.TextSpan, 0, len(lines))
	offset, captureIndex := 0, 0
	for _, line := range lines {
		lineEnd := offset + len(line)
		position := offset
		spans := make([]ui.TextSpan, 0, 4)
		for captureIndex < len(result.Spans) && result.Spans[captureIndex].End <= offset {
			captureIndex++
		}
		for next := captureIndex; next < len(result.Spans); next++ {
			capture := result.Spans[next]
			if capture.Start >= lineEnd {
				break
			}
			start := max(position, max(offset, capture.Start))
			end := min(lineEnd, capture.End)
			if start > position {
				spans = append(spans, syntaxTextSpan(result.Source[position:start], highlight.Text, semantic))
			}
			if end > start {
				spans = append(spans, syntaxTextSpan(result.Source[start:end], capture.Role, semantic))
				position = end
			}
		}
		if position < lineEnd {
			spans = append(spans, syntaxTextSpan(result.Source[position:lineEnd], highlight.Text, semantic))
		} else if line == "" {
			spans = append(spans, syntaxTextSpan("", highlight.Text, semantic))
		}
		rows = append(rows, spans)
		offset = lineEnd + 1
	}
	return rows
}

type workspaceDiffLineLayout struct {
	height int
	wrap   bool
}

func workspaceDiffLineWidget(line protocol.DiffLine, syntaxSpans []ui.TextSpan, selected, oldRangeSelected, newRangeSelected bool, theme ui.Theme, semantic SemanticTheme, onHover, gutterPress ui.VoidCallback, gutterMotion func(ui.EventContext, ui.Mouse), layouts ...workspaceDiffLineLayout) ui.Widget {
	layout := workspaceDiffLineLayout{height: 1}
	if len(layouts) > 0 {
		layout = layouts[0]
	}
	oldNumber, newNumber, marker := "", "", " "
	if line.OldLine != nil {
		oldNumber = fmt.Sprintf("%d", *line.OldLine)
	}
	if line.NewLine != nil {
		newNumber = fmt.Sprintf("%d", *line.NewLine)
	}
	contentBackground, gutterBackground := theme.Background, theme.Background
	switch line.Kind {
	case "addition":
		marker = "+"
		contentBackground = semantic.Token(kittheme.TokenDiffAddedContentBackground)
		gutterBackground = semantic.Token(kittheme.TokenDiffAddedLineNumberBg)
	case "deletion":
		marker = "−"
		contentBackground = semantic.Token(kittheme.TokenDiffRemovedContentBg)
		gutterBackground = semantic.Token(kittheme.TokenDiffRemovedLineNumberBg)
	}
	gutterForeground := theme.MutedForeground
	if line.Kind == "addition" || line.Kind == "deletion" {
		gutterForeground = theme.Foreground
	}
	gutterStyle := ui.Style{Foreground: gutterForeground, Background: gutterBackground}
	oldNumberStyle, newNumberStyle := gutterStyle, gutterStyle
	if oldRangeSelected {
		oldNumberStyle.Foreground = theme.Foreground
		oldNumberStyle.Attribute = ui.AttrBold
	}
	if newRangeSelected {
		newNumberStyle.Foreground = theme.Foreground
		newNumberStyle.Attribute = ui.AttrBold
	}
	contentStyle := ui.Style{Foreground: theme.Foreground, Background: contentBackground}
	if len(syntaxSpans) == 0 {
		syntaxSpans = []ui.TextSpan{{Text: highlight.Sanitize(line.Content), Style: contentStyle}}
	} else {
		syntaxSpans = append([]ui.TextSpan(nil), syntaxSpans...)
		for index := range syntaxSpans {
			syntaxSpans[index].Style.Background = contentBackground
		}
	}
	gutter := workspaceDiffGutter(ui.RichText{Spans: []ui.TextSpan{
		{Text: fmt.Sprintf("%5s", oldNumber), Style: oldNumberStyle},
		{Text: " ", Style: gutterStyle},
		{Text: fmt.Sprintf("%5s", newNumber), Style: newNumberStyle},
		{Text: " ", Style: gutterStyle},
		{Text: marker + " ", Style: gutterStyle},
	}, SoftWrap: false}, 14, selected, theme, gutterPress, gutterMotion)
	content := ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: contentBackground}},
		ui.RichText{Spans: syntaxSpans, SoftWrap: layout.wrap},
	)
	row := ui.Widget(ui.SizedBox{Height: layout.height, Child: ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStart, Children: []ui.Widget{
		gutter,
		ui.Expanded(content),
	}}})
	return mouseActivator{Child: row, OnHover: onHover, OnPressed: onHover}
}

func (s *workspaceDiffPaneState) footerText() string {
	w := s.Widget().(workspaceDiffPane)
	if s.active.commenting {
		return "Inline comment " + glyphMiddleDot + " enter save " + glyphMiddleDot + " shift+enter newline " + glyphMiddleDot + " esc cancel"
	}
	if s.isFrozen(w) {
		return "Frozen evidence " + glyphMiddleDot + " navigation only"
	}
	horizontalHint := "←→ columns"
	selectionHint := "v range"
	commentHint := "c note"
	if anchor, ok := s.active.selectedDiffAnchor(); s.active.selectionActive() && ok {
		selectionHint = "v clear"
		commentHint = fmt.Sprintf("c %s L%d–%d", anchor.Side, anchor.StartLine, anchor.EndLine)
	}
	parts := []string{"↑↓ lines", horizontalHint, selectionHint, commentHint, "[ ] files", "{ } hunks", "r refresh", "g target", "G choose"}
	if s.changesAvailable {
		parts = append([]string{"Changes available"}, parts...)
	}
	if _, ok := s.active.annotationAtCursor(w); ok {
		parts = append(parts[:4], append([]string{"e edit", "d remove"}, parts[4:]...)...)
	}
	wrapHint := "w wrap"
	if s.wrapLines {
		wrapHint = "w clip"
	}
	parts = append(parts[:2], append([]string{wrapHint}, parts[2:]...)...)

	if s.active.loadingMore {
		parts = append([]string{"Loading more diff content…"}, parts...)
	} else if s.active.fileNextCursor != "" {
		parts = append([]string{"m more"}, parts...)
	}
	if s.observationCursor != "" {
		label := "More changed files available"
		if s.observationError != "" {
			label = "Changed-file list incomplete · r retry"
		}
		parts = append([]string{label}, parts...)
	}
	if !s.observation.Complete && s.observation.Revision != "" {
		parts = append([]string{"Incomplete observation"}, parts...)
	}
	return strings.Join(parts, " "+glyphMiddleDot+" ")
}

// toggleWrapLines shares layout invalidation and persistence for mouse and keys.
func (s *workspaceDiffPaneState) toggleWrapLines() {
	next := !s.wrapLines
	s.SetState(func() {
		s.wrapLines = next
		s.invalidateDocumentLayout()
	})
	if callback := s.Widget().(workspaceDiffPane).OnWrapLinesChanged; callback != nil {
		callback(next)
	}
}
