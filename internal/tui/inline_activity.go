package tui

import (
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

const inlineActivityMaxRows = 12

type inlineActivityWindow struct {
	ID                    string
	Source                transcriptDisplayItem
	Controller            *ui.ScrollController
	List                  *activityListController
	States                map[transcriptToolStateKey]transcriptMessage
	Expanded              map[activityToolKey]bool
	Cursor                activityToolKey
	OuterScroll           *ui.ScrollController
	SubagentConversations []protocol.SubagentConversation
	OnToggleTool          func(ui.EventContext, activityToolKey)
	OnSelectTool          func(ui.EventContext, activityToolKey)
	OnOpenSubagent        func(ui.EventContext, string)
}

func (w inlineActivityWindow) WidgetKey() ui.KeyValue { return ui.KeyValue("inline-activity:" + w.ID) }
func (inlineActivityWindow) CreateState() ui.State    { return &inlineActivityWindowState{} }

type inlineActivityWindowState struct {
	ui.StateBase
	scroll       ui.ScrollController
	list         activityListController
	contentRows  int
	viewportRows int
	needsMeasure bool
	needsEnd     bool
}

func (s *inlineActivityWindowState) InitState() {
	s.viewportRows = 1
	s.needsMeasure = true
	s.needsEnd = true
}

func (s *inlineActivityWindowState) DidUpdateWidget(old ui.Widget) {
	previous := old.(inlineActivityWindow)
	current := s.Widget().(inlineActivityWindow)
	if previous.ID != current.ID || transcriptActivityInProgress(
		transcriptPresentation{Items: []transcriptDisplayItem{current.Source}, ToolStates: current.States}, current.Source.ID,
	) {
		s.needsMeasure = true
		scroll := &s.scroll
		if current.Controller != nil {
			scroll = current.Controller
		}
		s.needsEnd = scrollControllerPinnedToEnd(scroll)
	}
}

func (s *inlineActivityWindowState) TickFrame(time.Time) bool {
	window := s.Widget().(inlineActivityWindow)
	scroll := &s.scroll
	if window.Controller != nil {
		scroll = window.Controller
	}
	if !scroll.Attached() {
		return s.needsMeasure || s.needsEnd
	}
	metrics := scroll.Metrics()
	rows := max(1, metrics.ContentHeight)
	viewport := min(inlineActivityMaxRows, rows)
	s.needsMeasure = false
	if rows != s.contentRows || viewport != s.viewportRows {
		s.SetState(func() {
			s.contentRows = rows
			s.viewportRows = viewport
		})
		return true
	}
	if s.needsEnd {
		scroll.ScrollToEnd()
		s.needsEnd = false
	}
	return false
}

func (s *inlineActivityWindowState) Build(ctx ui.BuildContext) ui.Widget {
	window := s.Widget().(inlineActivityWindow)
	theme := ui.MustDepend[ui.Theme](ctx)
	scroll := &s.scroll
	if window.Controller != nil {
		scroll = window.Controller
	}
	list := &s.list
	if window.List != nil {
		list = window.List
	}
	items := buildActivityListItems(window.Source)
	widgets := make([]ui.Widget, len(items))
	keys := make([]activityToolKey, len(items))
	view := shellView{Snapshot: shellSnapshot{
		ActivityExpanded:      window.Expanded,
		ActivityCursor:        window.Cursor,
		Scroll:                window.OuterScroll,
		SubagentConversations: window.SubagentConversations,
	}, Callbacks: shellCallbacks{
		ToggleActivityTool:   window.OnToggleTool,
		SelectActivityTool:   window.OnSelectTool,
		OpenSubagentFromTool: window.OnOpenSubagent,
	}}
	for index, item := range items {
		widgets[index] = view.activityListItem(theme, item, window.States)
		if row, ok := widgets[index].(activityToolRowWidget); ok {
			row.ContainerBackground = true
			widgets[index] = row
		}
		if item.Kind == activityListTool {
			keys[index] = item.Key
		}
	}
	body := activityList{Controller: list, OuterScroll: scroll, ToolKeys: keys, Children: widgets}
	return ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: theme.Surface}},
		ui.SizedBox{Height: s.viewportRows, Child: nestedScrollHandoff{
			Inner: scroll, Outer: window.OuterScroll,
			Child: ui.Scrollbar{Child: ui.CustomScrollView{
				Controller: scroll,
				Slivers:    []ui.Widget{ui.SliverToBox{Child: ui.Padding(ui.Symmetric(1, 0), body)}},
			}},
		}},
	)
}
