package tui

import (
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

type inlineActivityWindow struct {
	ID                    string
	Source                transcriptDisplayItem
	List                  *activityListController
	States                map[transcriptToolStateKey]transcriptMessage
	Expanded              map[activityToolKey]bool
	Cursor                activityToolKey
	OuterScroll           *ui.ScrollController
	SubagentConversations []protocol.SubagentConversation
	OnSelectTool          func(ui.EventContext, activityToolKey)
	OnOpenSubagent        func(ui.EventContext, string)
}

func (w inlineActivityWindow) WidgetKey() ui.KeyValue { return ui.KeyValue("inline-activity:" + w.ID) }
func (inlineActivityWindow) CreateState() ui.State    { return &inlineActivityWindowState{} }

type inlineActivityWindowState struct {
	ui.StateBase
	list activityListController
}

func (s *inlineActivityWindowState) Build(ctx ui.BuildContext) ui.Widget {
	window := s.Widget().(inlineActivityWindow)
	theme := ui.MustDepend[ui.Theme](ctx)
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
		SelectActivityTool:   window.OnSelectTool,
		OpenSubagentFromTool: window.OnOpenSubagent,
	}}
	for index, item := range items {
		widgets[index] = view.activityListItem(theme, item, window.States)
		if item.Kind == activityListTool {
			keys[index] = item.Key
		}
	}
	body := activityList{Controller: list, OuterScroll: window.OuterScroll, ToolKeys: keys, Children: widgets}
	return ui.Padding(ui.Symmetric(1, 0), body)
}
