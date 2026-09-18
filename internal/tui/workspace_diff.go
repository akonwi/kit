package tui

import (
	"context"
	"errors"
	"fmt"
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
	phase                    workspaceDiffPhase
	generation               uint64
	cancel                   context.CancelFunc
	observation              protocol.DiffObservation
	files                    []protocol.DiffFileSummary
	selectedFile             int
	hunks                    []protocol.DiffHunk
	cursorRow                int
	cursorSide               workspaceDiffSide
	wrapLines                bool
	splitColumn              int
	splitTargetsCached       []workspaceDiffSplitTarget
	splitNavigationCached    []workspaceDiffNavigationLine
	splitNavigationValid     bool
	splitTargetsWidth        int
	splitTargetsWrap         bool
	splitMaxColumn           int
	splitTargetsValid        bool
	lineRows                 []int
	hunkRows                 []int
	errorText                string
	loadingFile              bool
	loadingMore              bool
	fileNextCursor           string
	observationCursor        string
	scroll                   ui.ScrollPaneController
	viewportWidth            int
	cursorRevealPending      bool
	revealPendingLayout      bool
	focus                    ui.FocusNode
	appliedOpen              uint64
	resultMu                 sync.Mutex
	pendingObservation       *workspaceDiffObservationResult
	pendingPoll              *workspaceDiffPollResult
	pendingFile              *workspaceDiffFileResult
	highlightGeneration      uint64
	highlightCancel          context.CancelFunc
	pendingHighlight         *workspaceDiffHighlightResult
	oldHighlighted           highlight.Result
	newHighlighted           highlight.Result
	highlightReady           bool
	commenting               bool
	commentBody              string
	commentPending           bool
	commentLoading           bool
	commentError             string
	commentAnchor            protocol.WorkingTreeDiffAnnotationAnchor
	commentAnnotationID      uint64
	selectionAnchor          protocol.WorkingTreeDiffAnnotationAnchor
	mouseSelectionGeneration uint64
	commentLoadCancel        func()
	commentOperation         uint64
	pollCancel               context.CancelFunc
	pollRequestCancel        context.CancelFunc
	pollGeneration           uint64
	polling                  bool
	changesAvailable         bool
	refreshPath              string
	refreshSide              workspaceDiffSide
	refreshLine              int
	disposed                 bool
	activeTarget             protocol.DiffTargetEntry
	pendingTarget            protocol.DiffTargetEntry
	stagedObservation        protocol.DiffObservation
	catalog                  []protocol.DiffTargetEntry
	catalogLoading           bool
	catalogError             string
	catalogGeneration        uint64
	catalogCancel            context.CancelFunc
	pendingCatalog           *workspaceDiffCatalogResult
	targetPickerOpen         bool
	targetQuery              string
	targetSelection          int
	pinnedEvidence           bool
}

func (s *workspaceDiffPaneState) InitState() {
	w := s.Widget().(workspaceDiffPane)
	s.appliedOpen = w.Descriptor.OpenGeneration
	s.cursorSide = workspaceDiffSideNew
	s.wrapLines = w.InitialWrapLines
	if w.Presentation.Active && !s.isFrozen(w) {
		s.startDescriptorLoad()
	}
	s.syncPolling(w)
}

func (s *workspaceDiffPaneState) DidUpdateWidget(old ui.Widget) {
	previous := old.(workspaceDiffPane)
	w := s.Widget().(workspaceDiffPane)
	if previous.InitialWrapLines != w.InitialWrapLines && s.wrapLines != w.InitialWrapLines {
		s.wrapLines = w.InitialWrapLines
		if s.wrapLines {
			s.splitColumn = 0
		}
		s.invalidateSplitTargets()
		s.cursorRevealPending = true
		s.revealPendingLayout = true
	}
	s.syncPolling(w)
	if s.isFrozen(w) {
		s.stopWork()
		s.stopCatalog()
		return
	}
	if previous.Presentation.Active && !w.Presentation.Active {
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
	if !previous.Presentation.Active && w.Presentation.Active {
		switch {
		case s.phase == workspaceDiffInitial || s.phase == workspaceDiffLoading:
			s.startDescriptorLoad()
		case s.loadingFile:
			s.loadingFile = false
			s.startFileLoad()
		case s.loadingMore:
			s.loadingMore = false
			s.loadMoreFileDiff()
		case !s.highlightReady && len(s.hunks) > 0:
			s.startHighlight()
		}
	}
}

func (s *workspaceDiffPaneState) Dispose() {
	s.disposed = true
	s.stopPolling()
	if s.commentLoadCancel != nil {
		s.commentLoadCancel()
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
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
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
	s.stopHighlight()
}

func (s *workspaceDiffPaneState) stopHighlight() {
	if s.highlightCancel != nil {
		s.highlightCancel()
		s.highlightCancel = nil
		s.highlightGeneration++
	}
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
	s.hunks = nil
	s.cursorRow = 0
	s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
	if descriptor.DiffSide == "old" {
		s.cursorSide = workspaceDiffSideOld
	} else {
		s.cursorSide = workspaceDiffSideNew
	}
	s.requestFilePage("", false)
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
	s.stopWork()
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
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
	}()
}

func (s *workspaceDiffPaneState) completeObservation(page protocol.DiffPage, err error) {
	s.cancel = nil
	if err != nil {
		message := workspaceDiffErrorText(err)
		s.pendingTarget = protocol.DiffTargetEntry{}
		if s.observation.Revision == "" {
			s.phase = workspaceDiffError
			s.errorText = message
		} else {
			s.loadingFile = false
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
		s.hunks = nil
		s.selectedFile = 0
		s.cursorRow = 0
		s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
		s.scroll = ui.ScrollPaneController{}
		s.invalidateSplitTargets()
		s.highlightReady = false
		s.phase = workspaceDiffReady
		s.loadingFile = len(s.files) > 0
	} else {
		s.files = append(s.files, page.Files...)
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
		s.loadingFile = false
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
		s.refreshLine = 0
	}
	s.refreshPath = ""
	s.loadingFile = false
	s.startFileLoad()
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
	return s.commenting || s.commentPending || s.commentLoading || s.selectionActive()
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
	if line, ok := s.lineAtRow(s.cursorRow); ok {
		coordinate := line.NewLine
		if s.cursorSide == workspaceDiffSideOld {
			coordinate = line.OldLine
		}
		if coordinate != nil {
			s.refreshSide = s.cursorSide
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
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
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
	s.loadingFile = false
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

func (s *workspaceDiffPaneState) startFileLoad() {
	s.hunks = nil
	s.invalidateSplitTargets()
	s.highlightReady = false
	s.oldHighlighted = highlight.Result{}
	s.newHighlighted = highlight.Result{}
	s.cursorRow = 0
	s.lineRows = nil
	s.hunkRows = nil
	s.fileNextCursor = ""
	s.requestFilePage("", false)
}

func (s *workspaceDiffPaneState) requestFilePage(cursor string, appendPage bool) {
	w := s.Widget().(workspaceDiffPane)
	if w.Diff == nil || !w.Presentation.Active || s.isFrozen(w) || s.selectedFile < 0 || s.selectedFile >= len(s.files) {
		return
	}
	s.stopWork()
	s.generation++
	generation := s.generation
	s.loadingFile = !appendPage
	s.loadingMore = appendPage
	file := s.files[s.selectedFile]
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	annotationID := uint64(0)
	if s.pinnedEvidence {
		annotationID = w.Descriptor.AnnotationID
	}
	input := protocol.ReadFileDiffInput{
		TargetID: s.observation.Target.ID, TargetRevision: s.observation.Revision,
		Path: file.Path, ExpectedFileRevision: file.FileRevision, AnnotationID: annotationID, Cursor: cursor,
	}
	go func() {
		page, err := w.Diff.ReadFileDiff(ctx, input)
		if ctx.Err() != nil {
			return
		}
		s.resultMu.Lock()
		if s.pendingFile == nil || generation >= s.pendingFile.generation {
			s.pendingFile = &workspaceDiffFileResult{generation: generation, page: page, append: appendPage, err: err}
		}
		s.resultMu.Unlock()
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
	}()
}

func (s *workspaceDiffPaneState) completeFileLoad(page protocol.FileDiffPage, appendPage bool, err error) {
	s.cancel = nil
	s.loadingFile = false
	s.loadingMore = false
	if err != nil {
		s.errorText = workspaceDiffErrorText(err)
		return
	}
	s.errorText = ""
	if appendPage {
		s.appendHunks(page.Hunks)
	} else {
		s.observation = page.Observation
		if s.selectedFile >= 0 && s.selectedFile < len(s.files) {
			s.files[s.selectedFile] = page.File
		}
		s.hunks = append([]protocol.DiffHunk(nil), page.Hunks...)
	}
	s.invalidateSplitTargets()
	s.fileNextCursor = page.NextCursor
	s.rebuildHunkRows()
	descriptor := s.Widget().(workspaceDiffPane).Descriptor
	refreshReveal := s.refreshLine > 0
	if refreshReveal {
		descriptor.DiffSide = "new"
		if s.refreshSide == workspaceDiffSideOld {
			descriptor.DiffSide = "old"
		}
		descriptor.RevealStartLine = s.refreshLine
		descriptor.RevealEndLine = s.refreshLine
		s.cursorSide = s.refreshSide
	}
	foundRevealStart, foundRevealEnd := false, descriptor.RevealEndLine <= 0 || descriptor.RevealEndLine == descriptor.RevealStartLine
	if reveal := descriptor.RevealStartLine; reveal > 0 {
		row := 0
		found := false
		for _, hunk := range s.hunks {
			row++
			for _, line := range hunk.Lines {
				matches := descriptor.DiffSide == "old" && line.OldLine != nil && *line.OldLine == reveal || descriptor.DiffSide == "new" && line.NewLine != nil && *line.NewLine == reveal
				if descriptor.DiffSide == "" {
					matches = line.OldLine != nil && *line.OldLine == reveal || line.NewLine != nil && *line.NewLine == reveal
				}
				if matches {
					s.setCursorRow(row)
					found = true
					break
				}
				row++
			}
			if found {
				break
			}
		}
		foundRevealStart = found
		if end := descriptor.RevealEndLine; end > 0 {
			for _, hunk := range s.hunks {
				for _, line := range hunk.Lines {
					foundRevealEnd = foundRevealEnd || descriptor.DiffSide == "old" && line.OldLine != nil && *line.OldLine == end || descriptor.DiffSide == "new" && line.NewLine != nil && *line.NewLine == end
					if descriptor.DiffSide == "" {
						foundRevealEnd = foundRevealEnd || line.OldLine != nil && *line.OldLine == end || line.NewLine != nil && *line.NewLine == end
					}
				}
			}
		}
	}
	if (descriptor.DiffTargetID != "" || refreshReveal) && descriptor.RevealStartLine > 0 && (!foundRevealStart || !foundRevealEnd) && s.fileNextCursor != "" {
		s.requestFilePage(s.fileNextCursor, true)
		return
	}
	if s.cursorRow == 0 && len(s.hunkRows) > 0 {
		s.setCursorRow(s.hunkRows[0])
	}
	if refreshReveal {
		s.refreshLine = 0
	}
	s.startHighlight()
}

func (s *workspaceDiffPaneState) startHighlight() {
	w := s.Widget().(workspaceDiffPane)
	if !w.Presentation.Active || s.isFrozen(w) || len(s.hunks) == 0 || s.selectedFile < 0 || s.selectedFile >= len(s.files) {
		return
	}
	s.stopHighlight()
	s.highlightReady = false
	s.highlightGeneration++
	generation := s.highlightGeneration
	oldSource, newSource := workspaceDiffHighlightSources(s.hunks)
	file := s.files[s.selectedFile]
	ctx, cancel := context.WithCancel(context.Background())
	s.highlightCancel = cancel
	dispatch := w.Dispatch
	if dispatch == nil {
		dispatch = s.Context().Runtime().Dispatch
	}
	highlighter := w.Highlighter
	if highlighter == nil {
		highlighter = highlight.Default
	}
	go func() {
		oldResult := highlight.Validate(highlighter.Highlight(ctx, highlight.Request{Path: file.Path, Source: oldSource, Revision: file.FileRevision + ":old"}))
		if ctx.Err() != nil {
			return
		}
		newResult := highlight.Validate(highlighter.Highlight(ctx, highlight.Request{Path: file.Path, Source: newSource, Revision: file.FileRevision + ":new"}))
		if ctx.Err() != nil {
			return
		}
		s.resultMu.Lock()
		if s.pendingHighlight == nil || generation >= s.pendingHighlight.generation {
			s.pendingHighlight = &workspaceDiffHighlightResult{generation: generation, old: oldResult, new: newResult}
		}
		s.resultMu.Unlock()
		dispatch(func() {
			if !s.disposed {
				s.SetState(func() {})
			}
		})
	}()
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

func (s *workspaceDiffPaneState) appendHunks(next []protocol.DiffHunk) {
	if len(s.hunks) > 0 && len(next) > 0 && next[0].ContinuedBefore && s.hunks[len(s.hunks)-1].ContinuedAfter {
		last := &s.hunks[len(s.hunks)-1]
		last.Lines = append(last.Lines, next[0].Lines...)
		last.ContinuedAfter = next[0].ContinuedAfter
		next = next[1:]
	}
	s.hunks = append(s.hunks, next...)
}

func (s *workspaceDiffPaneState) rebuildHunkRows() {
	s.invalidateSplitTargets()
	s.hunkRows = s.hunkRows[:0]
	s.lineRows = s.lineRows[:0]
	row := 0
	for _, hunk := range s.hunks {
		s.hunkRows = append(s.hunkRows, row+1)
		for line := range hunk.Lines {
			s.lineRows = append(s.lineRows, row+line+1)
		}
		row += len(hunk.Lines) + 1
	}
}

func (s *workspaceDiffPaneState) setCursorRow(row int) {
	s.cursorRow = row
	if line, ok := s.lineAtRow(row); ok {
		switch line.Kind {
		case "deletion":
			s.cursorSide = workspaceDiffSideOld
		case "addition":
			s.cursorSide = workspaceDiffSideNew
		}
	}
}

func (s *workspaceDiffPaneState) lineAtRow(wanted int) (protocol.DiffLine, bool) {
	row := 0
	for _, hunk := range s.hunks {
		row++
		for _, line := range hunk.Lines {
			if row == wanted {
				return line, true
			}
			row++
		}
	}
	return protocol.DiffLine{}, false
}

func (s *workspaceDiffPaneState) moveRangeLine(delta int) {
	if delta == 0 || !s.selectionActive() {
		return
	}
	side := s.selectionAnchor.Side
	rows := make([]int, 0, len(s.lineRows))
	row := 0
	for _, hunk := range s.hunks {
		row++
		for _, line := range hunk.Lines {
			coordinate := line.NewLine
			if side == "old" {
				coordinate = line.OldLine
			}
			if coordinate != nil {
				rows = append(rows, row)
			}
			row++
		}
	}
	selected := 0
	for index, candidate := range rows {
		if candidate == s.cursorRow {
			selected = index
			break
		}
	}
	next := max(0, min(len(rows)-1, selected+delta))
	cursorSide := workspaceDiffSideNew
	if side == "old" {
		cursorSide = workspaceDiffSideOld
	}
	if next != selected && s.setRangeCursor(rows[next], cursorSide) {
		s.revealCursor()
	}
	if delta > 0 && len(rows)-next <= 10 {
		s.loadMoreFileDiff()
	}
}

func (s *workspaceDiffPaneState) moveLine(delta int) {
	if s.selectionActive() {
		s.moveRangeLine(delta)
		return
	}
	if s.viewportWidth >= workspaceDiffSplitBreakpoint {
		s.moveSplitLine(delta)
	} else {
		if len(s.lineRows) == 0 || delta == 0 {
			return
		}
		selected := 0
		for index, row := range s.lineRows {
			if row <= s.cursorRow {
				selected = index
			}
		}
		selected = max(0, min(len(s.lineRows)-1, selected+delta))
		s.setCursorRow(s.lineRows[selected])
		s.revealCursor()
		if delta > 0 && len(s.lineRows)-selected <= 10 {
			s.loadMoreFileDiff()
		}
	}
}

type workspaceDiffSplitTarget struct {
	oldRow, newRow int
	visualRow      int
	height         int
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

func (s *workspaceDiffPaneState) invalidateSplitTargets() {
	s.splitTargetsCached = nil
	s.splitNavigationCached = nil
	s.splitNavigationValid = false
	s.splitMaxColumn = 0
	s.splitTargetsValid = false
}

func (s *workspaceDiffPaneState) splitTargets() []workspaceDiffSplitTarget {
	if s.splitTargetsValid && s.splitTargetsWidth == s.viewportWidth && s.splitTargetsWrap == s.wrapLines {
		return s.splitTargetsCached
	}
	type sourceLine struct {
		row  int
		line protocol.DiffLine
	}
	targets := make([]workspaceDiffSplitTarget, 0, len(s.lineRows))
	row, visualRow := 0, 0
	for _, hunk := range s.hunks {
		row++
		visualRow++
		for index := 0; index < len(hunk.Lines); {
			if hunk.Lines[index].Kind == "context" {
				line := hunk.Lines[index]
				height := workspaceDiffSplitPairHeight(&line, &line, s.viewportWidth, s.wrapLines)
				targets = append(targets, workspaceDiffSplitTarget{oldRow: row, newRow: row, visualRow: visualRow, height: height})
				row++
				visualRow += height
				index++
				continue
			}
			end := index
			deletions, additions := make([]sourceLine, 0), make([]sourceLine, 0)
			for end < len(hunk.Lines) && hunk.Lines[end].Kind != "context" {
				item := sourceLine{row: row, line: hunk.Lines[end]}
				if item.line.Kind == "deletion" {
					deletions = append(deletions, item)
				} else {
					additions = append(additions, item)
				}
				row++
				end++
			}
			for pair := 0; pair < max(len(deletions), len(additions)); pair++ {
				target := workspaceDiffSplitTarget{oldRow: -1, newRow: -1, visualRow: visualRow, height: 1}
				var oldLine, newLine *protocol.DiffLine
				if pair < len(deletions) {
					target.oldRow = deletions[pair].row
					oldLine = &deletions[pair].line
				}
				if pair < len(additions) {
					target.newRow = additions[pair].row
					newLine = &additions[pair].line
				}
				target.height = workspaceDiffSplitPairHeight(oldLine, newLine, s.viewportWidth, s.wrapLines)
				targets = append(targets, target)
				visualRow += target.height
			}
			index = end
		}
	}
	oldWidth, newWidth := workspaceDiffSplitWidths(s.viewportWidth)
	maximumColumn := 0
	for _, hunk := range s.hunks {
		for _, line := range hunk.Lines {
			contentCells := uucode.StringWidth(highlight.Sanitize(line.Content))
			if line.OldLine != nil {
				maximumColumn = max(maximumColumn, contentCells-max(1, oldWidth-workspaceDiffSideGutterWidth(line, false)))
			}
			if line.NewLine != nil {
				maximumColumn = max(maximumColumn, contentCells-max(1, newWidth-workspaceDiffSideGutterWidth(line, true)))
			}
		}
	}
	s.splitTargetsCached = targets
	s.splitMaxColumn = max(0, maximumColumn)
	s.splitTargetsWidth = s.viewportWidth
	s.splitTargetsWrap = s.wrapLines
	s.splitTargetsValid = true
	return targets
}

type workspaceDiffNavigationLine struct {
	row       int
	side      workspaceDiffSide
	forceSide bool
}

func (s *workspaceDiffPaneState) splitNavigationLines() []workspaceDiffNavigationLine {
	if s.splitNavigationValid {
		return s.splitNavigationCached
	}
	lines := make([]workspaceDiffNavigationLine, 0, len(s.lineRows))
	row := 0
	for _, hunk := range s.hunks {
		row++
		for _, line := range hunk.Lines {
			target := workspaceDiffNavigationLine{row: row}
			switch line.Kind {
			case "deletion":
				target.side = workspaceDiffSideOld
				target.forceSide = true
			case "addition":
				target.side = workspaceDiffSideNew
				target.forceSide = true
			}
			lines = append(lines, target)
			row++
		}
	}
	s.splitNavigationCached = lines
	s.splitNavigationValid = true
	return lines
}

func (s *workspaceDiffPaneState) moveSplitLine(delta int) {
	lines := s.splitNavigationLines()
	if len(lines) == 0 || delta == 0 {
		return
	}
	selected := 0
	for index, line := range lines {
		if line.row == s.cursorRow {
			selected = index
			break
		}
	}
	selected = max(0, min(len(lines)-1, selected+delta))
	target := lines[selected]
	s.cursorRow = target.row
	if target.forceSide {
		s.cursorSide = target.side
	}
	s.revealCursor()
	if delta > 0 && len(lines)-selected <= 10 {
		s.loadMoreFileDiff()
	}
}

func (s *workspaceDiffPaneState) panSplit(columns int) {
	if s.wrapLines || columns == 0 {
		return
	}
	s.splitColumn = max(0, min(s.maxSplitColumn(), s.splitColumn+columns))
}

func (s *workspaceDiffPaneState) maxSplitColumn() int {
	s.splitTargets()
	return s.splitMaxColumn
}

func (s *workspaceDiffPaneState) loadMoreFileDiff() {
	if s.fileNextCursor != "" && !s.loadingMore {
		s.requestFilePage(s.fileNextCursor, true)
	}
}

func (s *workspaceDiffPaneState) annotationHeightAfter(side string, line int) int {
	w := s.Widget().(workspaceDiffPane)
	height := 0
	for _, annotation := range w.Annotations {
		anchor := annotation.Anchor.WorkingTreeDiff
		if !annotation.Stale && s.annotationMatches(anchor, side, line) && anchor.EndLine == line && !(s.commenting && annotation.ID == s.commentAnnotationID) {
			height += len(workspaceAnnotationBodyLines(annotation.BodyPreview))
		}
	}
	if s.commenting && s.commentAnchor.Side == side && s.commentAnchor.EndLine == line {
		height += s.diffCommentEditorHeight()
	}
	return height
}

func (s *workspaceDiffPaneState) cursorVisualRow() int {
	if s.viewportWidth < workspaceDiffSplitBreakpoint {
		logicalRow, physicalRow := 0, 0
		for _, hunk := range s.hunks {
			logicalRow++
			physicalRow++
			for _, line := range hunk.Lines {
				if logicalRow == s.cursorRow {
					return physicalRow
				}
				logicalRow++
				physicalRow++
				if line.OldLine != nil {
					physicalRow += s.annotationHeightAfter("old", *line.OldLine)
				}
				if line.NewLine != nil {
					physicalRow += s.annotationHeightAfter("new", *line.NewLine)
				}
			}
		}
		return physicalRow
	}
	extra := 0
	for _, target := range s.splitTargets() {
		if s.cursorSide == workspaceDiffSideOld && target.oldRow == s.cursorRow || s.cursorSide == workspaceDiffSideNew && target.newRow == s.cursorRow {
			return target.visualRow + extra
		}
		if target.oldRow >= 0 {
			if line, ok := s.lineAtRow(target.oldRow); ok && line.OldLine != nil {
				extra += s.annotationHeightAfter("old", *line.OldLine)
			}
		}
		if target.newRow >= 0 {
			if line, ok := s.lineAtRow(target.newRow); ok && line.NewLine != nil {
				extra += s.annotationHeightAfter("new", *line.NewLine)
			}
		}
	}
	return extra
}

func (s *workspaceDiffPaneState) revealCursor() {
	if !s.scroll.Attached() {
		return
	}
	vertical := s.scroll.Metrics(ui.ScrollVertical)
	viewportHeight := max(1, vertical.ViewportHeight)
	row, target := s.cursorVisualRow(), vertical.ScrollOffset
	if row < vertical.ScrollOffset {
		target = row
	} else if row >= vertical.ScrollOffset+viewportHeight {
		target = row - viewportHeight + 1
	}
	horizontal := s.horizontalOffset()
	currentHorizontal := s.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset
	if target != vertical.ScrollOffset || horizontal != currentHorizontal {
		s.scroll.ScrollTo(horizontal, max(0, target))
	}
}

func (s *workspaceDiffPaneState) TickFrame(time.Time) bool {
	if !s.cursorRevealPending {
		return false
	}
	if s.revealPendingLayout {
		s.revealPendingLayout = false
		return true
	}
	s.cursorRevealPending = false
	s.revealCursor()
	return false
}

func (s *workspaceDiffPaneState) horizontalOffset() int {
	if s.viewportWidth >= workspaceDiffSplitBreakpoint || !s.scroll.Attached() {
		return 0
	}
	return s.scroll.Metrics(ui.ScrollHorizontal).ScrollOffset
}

func (s *workspaceDiffPaneState) moveFile(delta int) {
	if len(s.files) == 0 || delta == 0 {
		return
	}
	if s.selectionActive() {
		s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
		if s.changesAvailable {
			s.applyAvailableRefresh()
			return
		}
	}
	next := (s.selectedFile + delta) % len(s.files)
	if next < 0 {
		next += len(s.files)
	}
	s.selectedFile = next
	s.scroll = ui.ScrollPaneController{}
	s.startFileLoad()
}

func (s *workspaceDiffPaneState) moveHunk(delta int) {
	if s.selectionActive() {
		s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
		if s.changesAvailable {
			s.applyAvailableRefresh()
			return
		}
	}
	if len(s.hunkRows) == 0 || delta == 0 {
		return
	}
	selected := 0
	for index, row := range s.hunkRows {
		if row <= s.cursorRow {
			selected = index
		}
	}
	selected = max(0, min(len(s.hunkRows)-1, selected+delta))
	s.setCursorRow(s.hunkRows[selected])
	s.revealCursor()
	if delta > 0 && selected == len(s.hunkRows)-1 {
		s.loadMoreFileDiff()
	}
}

func (s *workspaceDiffPaneState) applyPendingResults() {
	s.resultMu.Lock()
	observation := s.pendingObservation
	poll := s.pendingPoll
	file := s.pendingFile
	catalog := s.pendingCatalog
	highlightResult := s.pendingHighlight
	s.pendingObservation = nil
	s.pendingPoll = nil
	s.pendingFile = nil
	s.pendingCatalog = nil
	s.pendingHighlight = nil
	s.resultMu.Unlock()
	if observation != nil && observation.generation == s.generation {
		s.completeObservation(observation.page, observation.err)
	}
	if poll != nil && poll.generation == s.pollGeneration {
		s.completePoll(poll.page, poll.err)
	}
	if file != nil && file.generation == s.generation {
		s.completeFileLoad(file.page, file.append, file.err)
	}
	if catalog != nil && catalog.generation == s.catalogGeneration {
		s.completeCatalog(catalog.catalog, catalog.err)
	}
	if highlightResult != nil && highlightResult.generation == s.highlightGeneration {
		s.highlightCancel = nil
		s.oldHighlighted = highlightResult.old
		s.newHighlighted = highlightResult.new
		s.highlightReady = true
	}
}

func (s *workspaceDiffPaneState) currentDiffAnchor() (protocol.WorkingTreeDiffAnnotationAnchor, bool) {
	if s.selectedFile < 0 || s.selectedFile >= len(s.files) || s.cursorRow <= 0 {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	line, ok := s.lineAtRow(s.cursorRow)
	if !ok {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	lineNumber := line.NewLine
	side := "new"
	if s.cursorSide == workspaceDiffSideOld {
		lineNumber = line.OldLine
		side = "old"
	}
	if lineNumber == nil {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	file := s.files[s.selectedFile]
	return protocol.WorkingTreeDiffAnnotationAnchor{
		TargetID: s.observation.Target.ID, TargetRevision: s.observation.Revision,
		Path: file.Path, FileRevision: file.FileRevision, Side: side,
		StartLine: *lineNumber, EndLine: *lineNumber,
	}, true
}

func (s *workspaceDiffPaneState) selectionActive() bool {
	return s.selectionAnchor.TargetID != ""
}

func (s *workspaceDiffPaneState) selectedDiffAnchor() (protocol.WorkingTreeDiffAnnotationAnchor, bool) {
	current, ok := s.currentDiffAnchor()
	if !ok {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	if !s.selectionActive() {
		return current, true
	}
	start := s.selectionAnchor
	if start.TargetID != current.TargetID || start.TargetRevision != current.TargetRevision || start.Path != current.Path || start.FileRevision != current.FileRevision || start.Side != current.Side {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	start.StartLine = min(start.StartLine, current.StartLine)
	start.EndLine = max(s.selectionAnchor.EndLine, current.EndLine)
	if start.EndLine-start.StartLine+1 > protocol.MaxAnnotationRangeLines {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	covered := make(map[int]struct{}, start.EndLine-start.StartLine+1)
	for _, hunk := range s.hunks {
		for _, line := range hunk.Lines {
			coordinate := line.NewLine
			if start.Side == "old" {
				coordinate = line.OldLine
			}
			if coordinate != nil && *coordinate >= start.StartLine && *coordinate <= start.EndLine {
				covered[*coordinate] = struct{}{}
			}
		}
	}
	if len(covered) != start.EndLine-start.StartLine+1 {
		return protocol.WorkingTreeDiffAnnotationAnchor{}, false
	}
	return start, true
}

func (s *workspaceDiffPaneState) setRangeCursor(row int, side workspaceDiffSide) bool {
	previousRow, previousSide := s.cursorRow, s.cursorSide
	s.cursorRow, s.cursorSide = row, side
	if _, ok := s.selectedDiffAnchor(); !ok {
		s.cursorRow, s.cursorSide = previousRow, previousSide
		return false
	}
	return true
}

func (s *workspaceDiffPaneState) lineInSelection(side string, line int) bool {
	if !s.selectionActive() {
		return false
	}
	anchor, ok := s.selectedDiffAnchor()
	return ok && anchor.Side == side && line >= anchor.StartLine && line <= anchor.EndLine
}

func (s *workspaceDiffPaneState) beginGutterRange(w workspaceDiffPane, row int, side workspaceDiffSide) {
	if s.commenting || s.commentPending || w.OnCreateAnnotation == nil {
		return
	}
	s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
	s.cursorRow, s.cursorSide = row, side
	anchor, ok := s.currentDiffAnchor()
	if !ok {
		return
	}
	s.selectionAnchor = anchor
	s.mouseSelectionGeneration = w.MouseGestures.Generation()
}

func (s *workspaceDiffPaneState) extendGutterRange(w workspaceDiffPane, row int, side workspaceDiffSide) {
	if !s.selectionActive() || w.MouseGestures.Generation() != s.mouseSelectionGeneration {
		return
	}
	expectedSide := workspaceDiffSideNew
	if s.selectionAnchor.Side == "old" {
		expectedSide = workspaceDiffSideOld
	}
	if side == expectedSide {
		s.setRangeCursor(row, side)
	}
}

func (s *workspaceDiffPaneState) finishGutterRange(w workspaceDiffPane) {
	if !s.selectionActive() || w.MouseGestures.ReleasedGeneration() != s.mouseSelectionGeneration {
		return
	}
	s.mouseSelectionGeneration = 0
	s.beginComment(w)
}

func (s *workspaceDiffPaneState) annotationAtCursor(w workspaceDiffPane) (protocol.AnnotationSummary, bool) {
	anchor, ok := s.currentDiffAnchor()
	if !ok {
		return protocol.AnnotationSummary{}, false
	}
	for _, annotation := range w.Annotations {
		candidate := annotation.Anchor.WorkingTreeDiff
		if candidate != nil && !annotation.Stale && candidate.TargetID == anchor.TargetID && candidate.TargetRevision == anchor.TargetRevision && candidate.Path == anchor.Path && candidate.FileRevision == anchor.FileRevision && candidate.Side == anchor.Side && anchor.StartLine >= candidate.StartLine && anchor.StartLine <= candidate.EndLine {
			return annotation, true
		}
	}
	return protocol.AnnotationSummary{}, false
}

func (s *workspaceDiffPaneState) beginComment(w workspaceDiffPane) {
	if s.commenting || w.OnCreateAnnotation == nil {
		return
	}
	anchor, ok := s.selectedDiffAnchor()
	if !ok {
		return
	}
	s.SetState(func() {
		s.commentOperation++
		s.commenting = true
		s.commentBody = ""
		s.commentPending = false
		s.commentLoading = false
		s.commentError = ""
		s.commentAnchor = anchor
		s.commentAnnotationID = 0
	})
}

func (s *workspaceDiffPaneState) beginEditComment(w workspaceDiffPane, annotation protocol.AnnotationSummary) {
	anchor := annotation.Anchor.WorkingTreeDiff
	if s.commenting || annotation.Stale || anchor == nil || w.OnLoadAnnotation == nil {
		return
	}
	var operation uint64
	s.SetState(func() {
		s.commentOperation++
		operation = s.commentOperation
		s.commenting = true
		s.commentBody = annotation.BodyPreview
		s.commentPending = false
		s.commentLoading = true
		s.commentError = ""
		s.commentAnchor = *anchor
		s.commentAnnotationID = annotation.ID
	})
	s.commentLoadCancel = w.OnLoadAnnotation(annotation.ID, func(body string, err error) {
		if s.disposed || operation != s.commentOperation {
			return
		}
		s.SetState(func() {
			s.commentLoadCancel = nil
			s.commentLoading = false
			if err != nil {
				s.commentError = annotationErrorText(err)
				return
			}
			s.commentBody = body
		})
	})
}

func (s *workspaceDiffPaneState) closeComment() {
	s.commentOperation++
	if s.commentLoadCancel != nil {
		s.commentLoadCancel()
		s.commentLoadCancel = nil
	}
	s.commenting = false
	s.commentBody = ""
	s.commentPending = false
	s.commentLoading = false
	s.commentError = ""
	s.commentAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
	s.commentAnnotationID = 0
	s.applyAvailableRefresh()
}

func (s *workspaceDiffPaneState) submitComment(w workspaceDiffPane, body string) {
	if !s.commenting || s.commentPending || s.commentLoading || strings.TrimSpace(body) == "" {
		return
	}
	s.SetState(func() {
		s.commentPending = true
		s.commentBody = body
		s.commentError = ""
	})
	done := func(err error) {
		if s.disposed {
			return
		}
		s.SetState(func() {
			if err != nil {
				s.commentPending = false
				s.commentError = annotationErrorText(err)
				return
			}
			s.closeComment()
			s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
			s.applyAvailableRefresh()
		})
	}
	if s.commentAnnotationID != 0 {
		if w.OnUpdateAnnotation == nil {
			done(errors.New("annotation updates are unavailable"))
			return
		}
		w.OnUpdateAnnotation(s.commentAnnotationID, body, done)
		return
	}
	anchor := s.commentAnchor
	w.OnCreateAnnotation(protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkingTreeDiff, WorkingTreeDiff: &anchor}, body, done)
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
	s.applyPendingResults()
	w := s.Widget().(workspaceDiffPane)
	theme := ui.MustDepend[ui.Theme](ctx)
	semantic, ok := ui.Depend[SemanticTheme](ctx)
	if !ok {
		semantic = semanticFallback(theme)
	}
	left, right := "No changed files", s.targetLabel()
	if s.pendingTarget.Reference != "" {
		left = "Loading files…"
		right = "Switching target…  " + glyphMiddleDot + "  " + right
	} else if len(s.files) > 0 && s.selectedFile >= 0 && s.selectedFile < len(s.files) {
		file := s.files[s.selectedFile]
		left = fmt.Sprintf("%d of %d  %s  %s", s.selectedFile+1, len(s.files), glyphMiddleDot, file.Path)
		if file.Additions != nil && file.Deletions != nil {
			left += fmt.Sprintf("  +%d −%d", *file.Additions, *file.Deletions)
		}
	}
	if s.isFrozen(w) {
		right = "Frozen  " + glyphMiddleDot + "  " + right
	}
	header := ui.Widget(workspacePanelHeader(theme, left, right))
	if w.Presentation.Active {
		header = mouseActivator{Child: header, DefaultMouseShape: true, OnPressed: func(event ui.EventContext) {
			if w.OnFocusRequest != nil {
				w.OnFocusRequest(event)
			}
			s.SetState(func() { s.openTargetPicker() })
		}}
	}
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
		shortcuts["g"] = toggleWorkspaceDiffTargetIntent{}
		shortcuts["Shift+g"] = openWorkspaceDiffTargetPickerIntent{}
		if s.commenting {
			bindings[ui.IntentType("vaxis.dismiss")] = func(ui.EventContext, ui.Intent) ui.EventResult {
				if !s.commentPending {
					s.SetState(func() { s.closeComment() })
				}
				return ui.EventHandled
			}
		} else if w.OnCreateAnnotation != nil {
			bindings[selectWorkspaceDiffRangeIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
				s.SetState(func() {
					if s.selectionActive() {
						s.selectionAnchor = protocol.WorkingTreeDiffAnnotationAnchor{}
						s.applyAvailableRefresh()
					} else if anchor, ok := s.currentDiffAnchor(); ok {
						s.selectionAnchor = anchor
					}
				})
				return ui.EventHandled
			}
			shortcuts["v"] = selectWorkspaceDiffRangeIntent{}
			bindings[commentWorkspaceDiffIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
				s.beginComment(w)
				return ui.EventHandled
			}
			shortcuts["c"] = commentWorkspaceDiffIntent{}
			if annotation, ok := s.annotationAtCursor(w); ok {
				if w.OnLoadAnnotation != nil {
					bindings[editWorkspaceDiffAnnotationIntent{}.IntentType()] = func(ui.EventContext, ui.Intent) ui.EventResult {
						s.beginEditComment(w, annotation)
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
		if !s.commenting {
			bindings[moveWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
				movement := intent.(moveWorkspaceDiffIntent)
				s.SetState(func() {
					if movement.lines != 0 {
						s.moveLine(movement.lines)
					}
					if movement.columns != 0 {
						if s.viewportWidth >= workspaceDiffSplitBreakpoint {
							s.panSplit(movement.columns)
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
				next := !s.wrapLines
				s.SetState(func() {
					s.wrapLines = next
					if s.wrapLines {
						s.splitColumn = 0
					}
					s.invalidateSplitTargets()
					s.cursorRevealPending = true
					s.revealPendingLayout = true
				})
				if w.OnWrapLinesChanged != nil {
					w.OnWrapLinesChanged(next)
				}
				return ui.EventHandled
			}
			bindings[panWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
				s.SetState(func() { s.panSplit(intent.(panWorkspaceDiffIntent).columns) })
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
						s.changesAvailable = false
						s.startObservation()
					})
					return ui.EventHandled
				}
				shortcuts["["] = moveWorkspaceDiffFileIntent{delta: -1}
				shortcuts["]"] = moveWorkspaceDiffFileIntent{delta: 1}
				shortcuts["r"] = refreshWorkspaceDiffIntent{}
				if s.fileNextCursor != "" && !s.loadingMore {
					bindings[loadMoreWorkspaceDiffIntent{}.IntentType()] = func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
						s.SetState(func() { s.loadMoreFileDiff() })
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
	content = mouseReleaseListener{Child: content, OnRelease: func(ui.EventContext) { s.finishGutterRange(w) }}
	content = mouseActivator{Child: content, DefaultMouseShape: true, OnScroll: func(_ ui.EventContext, mouse ui.Mouse) ui.EventResult {
		if s.viewportWidth < workspaceDiffSplitBreakpoint || s.wrapLines {
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
		s.SetState(func() { s.panSplit(delta) })
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
	case s.loadingFile:
		child = centeredWorkspaceDiffLoading(s.viewportWidth, "Loading file diff…", ui.Style{Foreground: theme.MutedForeground})
	case s.errorText != "":
		child = ui.Center(ui.Text{Value: s.errorText, Style: ui.Style{Foreground: theme.DangerText}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis})
	case len(s.hunks) == 0:
		message := "No textual changes"
		if len(s.files) > 0 {
			file := s.files[s.selectedFile]
			if file.ContentState != "text" {
				message = workspaceDiffContentStateText(file)
			}
		}
		child = ui.Center(ui.Text{Value: message, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	default:
		var rows []ui.Widget
		var contentWidth, contentHeight int
		if s.viewportWidth >= workspaceDiffSplitBreakpoint {
			rows, contentWidth, contentHeight = s.splitDiffRows(theme, semantic)
		} else {
			rows, contentWidth, contentHeight = s.diffRows(theme, semantic)
		}
		pane := ui.Widget(ui.ScrollPane{Controller: &s.scroll, Child: ui.SizedBox{
			Width: max(s.viewportWidth, contentWidth), Height: contentHeight,
			Child: ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows},
		}})
		pane = ui.Scrollbar{
			Child:      pane,
			ThumbStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarForeground)},
			TrackStyle: ui.Style{Foreground: semantic.Token(kittheme.TokenScrollbarBackground)},
		}
		child = pane
	}
	content := ui.Widget(ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{ui.Expanded(child)}})
	return widthProbe{WidthChanged: func(width int) {
		if width != s.viewportWidth {
			s.viewportWidth = width
			s.splitColumn = 0
			s.invalidateSplitTargets()
			s.cursorRevealPending = true
			s.revealPendingLayout = true
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

func (s *workspaceDiffPaneState) annotationMatches(anchor *protocol.WorkingTreeDiffAnnotationAnchor, side string, line int) bool {
	if anchor == nil || s.selectedFile < 0 || s.selectedFile >= len(s.files) {
		return false
	}
	file := s.files[s.selectedFile]
	return anchor.TargetID == s.observation.Target.ID && anchor.TargetRevision == s.observation.Revision &&
		anchor.Path == file.Path && anchor.FileRevision == file.FileRevision && anchor.Side == side && line >= anchor.StartLine && line <= anchor.EndLine
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

func (s *workspaceDiffPaneState) diffAnnotationRows(theme ui.Theme, w workspaceDiffPane, side string, line, width int) []ui.Widget {
	rows := make([]ui.Widget, 0)
	for _, annotation := range w.Annotations {
		anchor := annotation.Anchor.WorkingTreeDiff
		if annotation.Stale || !s.annotationMatches(anchor, side, line) || anchor.EndLine != line || annotation.ID == s.commentAnnotationID && s.commenting {
			continue
		}
		bodyLines := workspaceAnnotationBodyLines(annotation.BodyPreview)
		for index, bodyLine := range bodyLines {
			row := ui.Widget(ui.SizedBox{Width: width, Height: 1, Child: ui.Text{
				Value: "   " + glyphDiamond + " " + bodyLine,
				Style: ui.Style{Foreground: theme.AccentText, Background: theme.Surface}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis,
			}})
			if index == 0 && w.OnLoadAnnotation != nil {
				captured := annotation
				row = mouseActivator{Child: row, OnPressed: func(ui.EventContext) { s.beginEditComment(w, captured) }}
			}
			rows = append(rows, row)
		}
	}
	if s.commenting && s.commentAnchor.Side == side && s.commentAnchor.EndLine == line {
		rows = append(rows, s.diffCommentEditor(theme, w, width))
	}
	return rows
}

func (s *workspaceDiffPaneState) diffCommentEditorHeight() int {
	height := 5
	if s.commentPending || s.commentLoading {
		height++
	}
	if s.commentError != "" {
		height++
	}
	if s.changesAvailable {
		height++
	}
	return height
}

func (s *workspaceDiffPaneState) diffCommentEditor(theme ui.Theme, w workspaceDiffPane, width int) ui.Widget {
	height := s.diffCommentEditorHeight()
	children := []ui.Widget{ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Surface}}}
	if s.commentError != "" {
		children = append(children, ui.Text{Value: "Could not save: " + s.commentError, Style: ui.Style{Foreground: theme.DangerText}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis})
	}
	if s.changesAvailable {
		children = append(children, ui.Text{Value: "Changes available · refreshes after editing", Style: ui.Style{Foreground: theme.Warning}, MaxLines: 1})
	}
	if s.commentPending || s.commentLoading {
		status := "Saving…"
		if s.commentLoading {
			status = "Loading…"
		}
		children = append(children, ui.Text{Value: s.commentBody, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 2, SoftWrap: true}, spinnerWithLabel(status, ui.Style{Foreground: theme.MutedForeground}))
	} else {
		children = append(children, messageComposer{
			Value: s.commentBody, Placeholder: "Write a comment…", MaxHeight: 2,
			OnChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.commentBody, s.commentError = value, "" })
			},
			OnSubmitted: func(_ ui.EventContext, value string) { s.submitComment(w, value) },
		})
	}
	children = append(children,
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Surface}},
		ui.Text{Value: "enter save · shift+enter newline · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
	)
	return ui.SizedBox{Width: width, Height: height, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: theme.Surface}},
		ui.Padding(ui.Insets{Left: 3, Right: 1}, ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, MainAxisSize: ui.MainAxisSizeMin, Children: children}),
	)}
}

func (s *workspaceDiffPaneState) diffRows(theme ui.Theme, semantic SemanticTheme) ([]ui.Widget, int, int) {
	w := s.Widget().(workspaceDiffPane)
	rows := make([]ui.Widget, 0)
	visualRow, contentWidth, contentHeight := 0, 1, 0
	var oldSyntax, newSyntax [][]ui.TextSpan
	if s.highlightReady {
		oldSyntax = workspaceDiffSyntaxLines(s.oldHighlighted, semantic)
		newSyntax = workspaceDiffSyntaxLines(s.newHighlighted, semantic)
	}
	oldSyntaxLine, newSyntaxLine := 0, 0
	for _, hunk := range s.hunks {
		header := fmt.Sprintf("%s -%d,%d +%d,%d", glyphDiamond, hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
		contentWidth = max(contentWidth, uucode.StringWidth(header))
		rows = append(rows, ui.SizedBox{Height: 1, Child: ui.Text{Value: header, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}})
		visualRow++
		contentHeight++
		for _, line := range hunk.Lines {
			contentWidth = max(contentWidth, workspaceDiffUnifiedLineWidth(line))
			var syntaxSpans []ui.TextSpan
			switch line.Kind {
			case "deletion":
				if oldSyntaxLine < len(oldSyntax) {
					syntaxSpans = oldSyntax[oldSyntaxLine]
				}
			case "addition", "context":
				if newSyntaxLine < len(newSyntax) {
					syntaxSpans = newSyntax[newSyntaxLine]
				}
			}
			if line.OldLine != nil {
				oldSyntaxLine++
			}
			if line.NewLine != nil {
				newSyntaxLine++
			}
			row := visualRow
			moveCursor := func(ui.EventContext) {
				if s.cursorRow != row {
					s.SetState(func() {
						if !s.selectionActive() {
							s.setCursorRow(row)
							return
						}
						side := s.cursorSide
						if line.Kind == "deletion" {
							side = workspaceDiffSideOld
						} else if line.Kind == "addition" {
							side = workspaceDiffSideNew
						}
						s.setRangeCursor(row, side)
					})
				}
			}
			lineSide := s.cursorSide
			if line.Kind == "deletion" {
				lineSide = workspaceDiffSideOld
			} else if line.Kind == "addition" {
				lineSide = workspaceDiffSideNew
			}
			gutterPress := func(ui.EventContext) {
				s.SetState(func() { s.beginGutterRange(w, row, lineSide) })
			}
			gutterMotion := func(_ ui.EventContext, mouse ui.Mouse) {
				if mouse.Button == ui.MouseLeftButton {
					s.SetState(func() { s.extendGutterRange(w, row, lineSide) })
				}
			}
			oldRangeSelected := line.OldLine != nil && s.lineInSelection("old", *line.OldLine)
			newRangeSelected := line.NewLine != nil && s.lineInSelection("new", *line.NewLine)
			rows = append(rows, workspaceDiffLineWidget(line, syntaxSpans, row == s.cursorRow, oldRangeSelected, newRangeSelected, theme, semantic, moveCursor, gutterPress, gutterMotion))
			contentHeight++
			visualRow++
			annotationWidth := max(contentWidth, s.viewportWidth)
			if line.OldLine != nil {
				notes := s.diffAnnotationRows(theme, w, "old", *line.OldLine, annotationWidth)
				rows = append(rows, notes...)
				for _, note := range notes {
					if box, ok := note.(ui.SizedBox); ok {
						contentHeight += max(1, box.Height)
					} else {
						contentHeight++
					}
				}
			}
			if line.NewLine != nil {
				notes := s.diffAnnotationRows(theme, w, "new", *line.NewLine, annotationWidth)
				rows = append(rows, notes...)
				for _, note := range notes {
					if box, ok := note.(ui.SizedBox); ok {
						contentHeight += max(1, box.Height)
					} else {
						contentHeight++
					}
				}
			}
		}
	}
	return rows, contentWidth, contentHeight
}

type workspaceDiffRenderedLine struct {
	line   protocol.DiffLine
	row    int
	syntax []ui.TextSpan
}

func (s *workspaceDiffPaneState) splitDiffRows(theme ui.Theme, semantic SemanticTheme) ([]ui.Widget, int, int) {
	rows := make([]ui.Widget, 0)
	visualRow, contentWidth, contentHeight := 0, max(1, s.viewportWidth), 0
	oldWidth, newWidth := workspaceDiffSplitWidths(s.viewportWidth)
	targets, targetIndex := s.splitTargets(), 0
	var oldSyntax, newSyntax [][]ui.TextSpan
	if s.highlightReady {
		oldSyntax = workspaceDiffSyntaxLines(s.oldHighlighted, semantic)
		newSyntax = workspaceDiffSyntaxLines(s.newHighlighted, semantic)
	}
	oldSyntaxLine, newSyntaxLine := 0, 0
	for _, hunk := range s.hunks {
		header := fmt.Sprintf("%s -%d,%d +%d,%d", glyphDiamond, hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
		rows = append(rows, ui.SizedBox{Height: 1, Child: ui.Text{Value: header, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}})
		contentHeight++
		visualRow++
		decorated := make([]workspaceDiffRenderedLine, 0, len(hunk.Lines))
		for _, line := range hunk.Lines {
			item := workspaceDiffRenderedLine{line: line, row: visualRow}
			switch line.Kind {
			case "deletion":
				if oldSyntaxLine < len(oldSyntax) {
					item.syntax = oldSyntax[oldSyntaxLine]
				}
			case "addition", "context":
				if newSyntaxLine < len(newSyntax) {
					item.syntax = newSyntax[newSyntaxLine]
				}
			}
			if line.OldLine != nil {
				oldSyntaxLine++
			}
			if line.NewLine != nil {
				newSyntaxLine++
			}
			decorated = append(decorated, item)
			visualRow++
		}
		for index := 0; index < len(decorated); {
			if decorated[index].line.Kind == "context" {
				item := decorated[index]
				rowHeight := targets[targetIndex].height
				targetIndex++
				rowWidget := s.workspaceDiffSplitRow(&item, &item,
					item.row == s.cursorRow && s.cursorSide == workspaceDiffSideOld,
					item.row == s.cursorRow && s.cursorSide == workspaceDiffSideNew,
					rowHeight, theme, semantic)
				rows = append(rows, rowWidget)
				contentHeight += rowHeight
				if item.line.OldLine != nil {
					notes := s.diffAnnotationRows(theme, s.Widget().(workspaceDiffPane), "old", *item.line.OldLine, oldWidth)
					rows = append(rows, workspaceDiffSplitSupplementRows(notes, true, s.viewportWidth, theme)...)
					contentHeight += workspaceDiffWidgetsHeight(notes)
				}
				if item.line.NewLine != nil {
					notes := s.diffAnnotationRows(theme, s.Widget().(workspaceDiffPane), "new", *item.line.NewLine, newWidth)
					rows = append(rows, workspaceDiffSplitSupplementRows(notes, false, s.viewportWidth, theme)...)
					contentHeight += workspaceDiffWidgetsHeight(notes)
				}
				index++
				continue
			}
			end := index
			for end < len(decorated) && decorated[end].line.Kind != "context" {
				end++
			}
			deletions, additions := make([]workspaceDiffRenderedLine, 0), make([]workspaceDiffRenderedLine, 0)
			for _, item := range decorated[index:end] {
				if item.line.Kind == "deletion" {
					deletions = append(deletions, item)
				} else {
					additions = append(additions, item)
				}
			}
			for pair := 0; pair < max(len(deletions), len(additions)); pair++ {
				var oldLine, newLine *workspaceDiffRenderedLine
				if pair < len(deletions) {
					oldLine = &deletions[pair]
				}
				if pair < len(additions) {
					newLine = &additions[pair]
				}
				rowHeight := targets[targetIndex].height
				targetIndex++
				rowWidget := s.workspaceDiffSplitRow(oldLine, newLine,
					oldLine != nil && oldLine.row == s.cursorRow,
					newLine != nil && newLine.row == s.cursorRow,
					rowHeight, theme, semantic)
				rows = append(rows, rowWidget)
				contentHeight += rowHeight
				if oldLine != nil && oldLine.line.OldLine != nil {
					notes := s.diffAnnotationRows(theme, s.Widget().(workspaceDiffPane), "old", *oldLine.line.OldLine, oldWidth)
					rows = append(rows, workspaceDiffSplitSupplementRows(notes, true, s.viewportWidth, theme)...)
					contentHeight += workspaceDiffWidgetsHeight(notes)
				}
				if newLine != nil && newLine.line.NewLine != nil {
					notes := s.diffAnnotationRows(theme, s.Widget().(workspaceDiffPane), "new", *newLine.line.NewLine, newWidth)
					rows = append(rows, workspaceDiffSplitSupplementRows(notes, false, s.viewportWidth, theme)...)
					contentHeight += workspaceDiffWidgetsHeight(notes)
				}
			}
			index = end
		}
	}
	return rows, contentWidth, contentHeight
}

func (s *workspaceDiffPaneState) workspaceDiffSplitRow(oldLine, newLine *workspaceDiffRenderedLine, oldSelected, newSelected bool, height int, theme ui.Theme, semantic SemanticTheme) ui.Widget {
	w := s.Widget().(workspaceDiffPane)
	moveOld := func(ui.EventContext) {
		if oldLine != nil && (s.cursorRow != oldLine.row || s.cursorSide != workspaceDiffSideOld) {
			s.SetState(func() {
				if s.selectionActive() {
					s.setRangeCursor(oldLine.row, workspaceDiffSideOld)
				} else {
					s.cursorRow = oldLine.row
					s.cursorSide = workspaceDiffSideOld
				}
			})
		}
	}
	gutterPressOld := func(ui.EventContext) {
		if oldLine != nil {
			s.SetState(func() { s.beginGutterRange(w, oldLine.row, workspaceDiffSideOld) })
		}
	}
	gutterMotionOld := func(_ ui.EventContext, mouse ui.Mouse) {
		if oldLine != nil && mouse.Button == ui.MouseLeftButton {
			s.SetState(func() { s.extendGutterRange(w, oldLine.row, workspaceDiffSideOld) })
		}
	}
	moveNew := func(ui.EventContext) {
		if newLine != nil && (s.cursorRow != newLine.row || s.cursorSide != workspaceDiffSideNew) {
			s.SetState(func() {
				if s.selectionActive() {
					s.setRangeCursor(newLine.row, workspaceDiffSideNew)
				} else {
					s.cursorRow = newLine.row
					s.cursorSide = workspaceDiffSideNew
				}
			})
		}
	}
	gutterPressNew := func(ui.EventContext) {
		if newLine != nil {
			s.SetState(func() { s.beginGutterRange(w, newLine.row, workspaceDiffSideNew) })
		}
	}
	gutterMotionNew := func(_ ui.EventContext, mouse ui.Mouse) {
		if newLine != nil && mouse.Button == ui.MouseLeftButton {
			s.SetState(func() { s.extendGutterRange(w, newLine.row, workspaceDiffSideNew) })
		}
	}
	oldRangeSelected := oldLine != nil && oldLine.line.OldLine != nil && s.lineInSelection("old", *oldLine.line.OldLine)
	newRangeSelected := newLine != nil && newLine.line.NewLine != nil && s.lineInSelection("new", *newLine.line.NewLine)
	oldWidget := workspaceDiffSideWidget(oldLine, oldSelected, oldRangeSelected, false, height, s.wrapLines, s.splitColumn, theme, semantic, moveOld, gutterPressOld, gutterMotionOld)
	newWidget := workspaceDiffSideWidget(newLine, newSelected, newRangeSelected, true, height, s.wrapLines, s.splitColumn, theme, semantic, moveNew, gutterPressNew, gutterMotionNew)
	oldWidth, newWidth := workspaceDiffSplitWidths(s.viewportWidth)
	row := ui.SizedBox{Width: oldWidth + newWidth + 1, Height: height, Child: ui.Flex{Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Width: oldWidth, Height: height, Child: oldWidget},
		ui.Text{Value: strings.TrimSuffix(strings.Repeat(glyphTableSeparator+"\n", height), "\n"), Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: height},
		ui.SizedBox{Width: newWidth, Height: height, Child: newWidget},
	}}}
	return row
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

func workspaceDiffLineWidget(line protocol.DiffLine, syntaxSpans []ui.TextSpan, selected, oldRangeSelected, newRangeSelected bool, theme ui.Theme, semantic SemanticTheme, onHover, gutterPress ui.VoidCallback, gutterMotion func(ui.EventContext, ui.Mouse)) ui.Widget {
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
		ui.RichText{Spans: syntaxSpans, SoftWrap: false},
	)
	row := ui.Widget(ui.SizedBox{Height: 1, Child: ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
		gutter,
		ui.Expanded(content),
	}}})
	return mouseActivator{Child: row, OnHover: onHover, OnPressed: onHover}
}

func (s *workspaceDiffPaneState) footerText() string {
	w := s.Widget().(workspaceDiffPane)
	if s.commenting {
		return "Inline comment " + glyphMiddleDot + " enter save " + glyphMiddleDot + " shift+enter newline " + glyphMiddleDot + " esc cancel"
	}
	if s.isFrozen(w) {
		return "Frozen evidence " + glyphMiddleDot + " navigation only"
	}
	horizontalHint := "←→ columns"
	selectionHint := "v range"
	commentHint := "c note"
	if anchor, ok := s.selectedDiffAnchor(); s.selectionActive() && ok {
		selectionHint = "v clear"
		commentHint = fmt.Sprintf("c %s L%d–%d", anchor.Side, anchor.StartLine, anchor.EndLine)
	}
	parts := []string{"↑↓ lines", horizontalHint, selectionHint, commentHint, "[ ] files", "{ } hunks", "r refresh", "g target", "G choose"}
	if s.changesAvailable {
		parts = append([]string{"Changes available"}, parts...)
	}
	if _, ok := s.annotationAtCursor(w); ok {
		parts = append(parts[:4], append([]string{"e edit", "d remove"}, parts[4:]...)...)
	}
	if s.viewportWidth >= workspaceDiffSplitBreakpoint {
		parts[1] = "←→ pan"
		wrapHint := "w wrap"
		if s.wrapLines {
			wrapHint = "w clip"
			parts = append(parts[:2], append([]string{wrapHint}, parts[2:]...)...)
		} else {
			parts[1] = "←→/H/L pan"
			parts = append(parts[:2], append([]string{wrapHint}, parts[2:]...)...)
		}
	}
	if s.loadingMore {
		parts = append([]string{"Loading more diff content…"}, parts...)
	} else if s.fileNextCursor != "" {
		parts = append([]string{"m more"}, parts...)
	}
	if s.observationCursor != "" {
		parts = append([]string{"More changed files available"}, parts...)
	}
	if !s.observation.Complete && s.observation.Revision != "" {
		parts = append([]string{"Incomplete observation"}, parts...)
	}
	return strings.Join(parts, " "+glyphMiddleDot+" ")
}
