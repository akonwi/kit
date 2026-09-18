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

// Small live groups reveal their rows until the sixth tool call arrives.
const inlineActivityAutoExpandLimit = 5

func inlineActivityIsOpen(overrides map[string]bool, id string, count int, inProgress bool) bool {
	if open, exists := overrides[id]; exists {
		return open
	}
	return inProgress && count > 0 && count <= inlineActivityAutoExpandLimit
}

func activeToolSource(presentation transcriptPresentation, running bool) string {
	if !running || len(presentation.Items) == 0 {
		return ""
	}
	turnID := presentation.Items[len(presentation.Items)-1].TurnID
	for index := len(presentation.Items) - 1; index >= 0; index-- {
		item := presentation.Items[index]
		if item.TurnID != turnID {
			break
		}
		if item.Kind == transcriptDisplayTurnWork {
			return item.ID
		}
	}
	return ""
}

func activitySourceIsOpen(overrides map[string]bool, presentation transcriptPresentation, sourceID string, running bool) bool {
	for _, item := range presentation.Items {
		if item.ID != sourceID {
			continue
		}
		return inlineActivityIsOpen(overrides, sourceID, len(displayItemToolCalls(item)), toolGroupInProgress(item, presentation.ToolStates, activeToolSource(presentation, running)))
	}
	return overrides[sourceID]
}

func toolGroupInProgress(item transcriptDisplayItem, states map[transcriptToolStateKey]transcriptMessage, activeSource string) bool {
	for _, step := range item.Items {
		if step.Aborted {
			return false
		}
	}
	if item.ID != "" && item.ID == activeSource {
		return true
	}
	for _, call := range displayItemToolCalls(item) {
		state, exists := states[transcriptToolStateKey{TurnID: item.TurnID, ToolCallID: call.ID}]
		if !exists || state.Pending {
			return true
		}
	}
	return false
}
