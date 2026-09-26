package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	kitmarkdown "github.com/akonwi/kit/internal/markdown"
	"github.com/akonwi/kit/internal/protocol"

	"go.rockorager.dev/vaxis/ui"
)

// transcriptReadingSection is one jump target inside a long assistant message,
// mirroring the macOS transcript's reading navigation.
type transcriptReadingSection struct {
	// Block is the index of the section's first markdown block.
	Block int
	Title string
}

// transcriptReadingSections derives jump targets from a parsed assistant
// message. Each heading starts a section named by its text, and content before
// the first heading becomes an "Overview" section. A message with no headings
// has no sections.
func transcriptReadingSections(document kitmarkdown.Document) []transcriptReadingSection {
	sections := make([]transcriptReadingSection, 0)
	for index, block := range document.Blocks {
		if block.Kind == kitmarkdown.BlockHeading {
			sections = append(sections, transcriptReadingSection{Block: index, Title: strings.TrimSpace(markdownPlainText(block.Runs))})
			continue
		}
		if index == 0 {
			sections = append(sections, transcriptReadingSection{Block: 0, Title: "Overview"})
		}
	}
	if len(sections) < 2 {
		return nil
	}
	return sections
}

// transcriptReadingSelection reports which section the viewport is in.
// offsets are each section's row within the scroll content; viewportStart is
// the first visible row. The selected section is the last one that begins at or
// above the viewport, matching the macOS transcript.
func transcriptReadingSelection(offsets []int, viewportStart int) int {
	selected := 0
	for index, offset := range offsets {
		if offset <= viewportStart {
			selected = index
		}
	}
	return selected
}

// transcriptReadingSnapshot is the section navigation the transcript strip
// renders. Visible is false unless a tall assistant message fills the viewport.
type transcriptReadingSnapshot struct {
	Visible  bool
	Title    string
	Selected int
	Count    int
}

// observeTranscriptReading shows the section strip while an assistant message
// taller than the viewport occupies it, tracking the section at the viewport's
// top edge.
func (s *appState) observeTranscriptReading() {
	reading, sections := transcriptReadingSnapshot{}, []transcriptReadingSection(nil)
	if s.transcriptVisible {
		messages := append(append([]transcriptMessage(nil), s.messages...), s.liveMessages...)
		reading, sections = transcriptReadingAt(messages, &s.scroll, &s.transcriptList)
	}
	if !reading.Visible && s.transcriptReadingPickerOpen {
		s.transcriptReadingPickerOpen = false
	}
	s.transcriptReadingSections = sections
	if reading != s.transcriptReading {
		s.transcriptReading = reading
		s.MarkNeedsBuild()
	}
}

// observeSubagentReading applies the same strip to each retained subagent
// conversation tab.
func (s *appState) observeSubagentReading() {
	changed := false
	if s.subagentReading == nil {
		s.subagentReading = map[string]transcriptReadingSnapshot{}
	}
	if s.subagentReadingSections == nil {
		s.subagentReadingSections = map[string][]transcriptReadingSection{}
	}
	seen := map[string]bool{}
	for conversationID, controller := range s.subagentScrolls {
		seen[conversationID] = true
		conversation := protocol.SubagentConversation{ID: conversationID}
		for _, candidate := range s.subagentConversations {
			if candidate.ID == conversationID {
				conversation = candidate
				break
			}
		}
		messages := subagentPaneMessages(conversation, s.subagentTranscripts[conversationID], s.subagentLive[conversationID])
		reading, sections := transcriptReadingAt(messages, controller, s.subagentTranscriptLists[conversationID])
		if s.subagentReading[conversationID] != reading {
			if reading.Visible {
				s.subagentReading[conversationID] = reading
			} else {
				delete(s.subagentReading, conversationID)
			}
			changed = true
		}
		if reading.Visible {
			s.subagentReadingSections[conversationID] = sections
		} else {
			delete(s.subagentReadingSections, conversationID)
			if s.subagentReadingPickerID == conversationID {
				s.subagentReadingPickerID = ""
				changed = true
			}
		}
	}
	for conversationID := range s.subagentReading {
		if !seen[conversationID] {
			delete(s.subagentReading, conversationID)
			delete(s.subagentReadingSections, conversationID)
			if s.subagentReadingPickerID == conversationID {
				s.subagentReadingPickerID = ""
			}
			changed = true
		}
	}
	if s.subagentReadingPickerID != "" && s.subagentReadingPickerID != s.subagentPaneID {
		s.subagentReadingPickerID = ""
		changed = true
	}
	if changed {
		s.MarkNeedsBuild()
	}
}

// transcriptReadingAt reports the section strip for one transcript viewport.
func transcriptReadingAt(messages []transcriptMessage, scroll *ui.ScrollController, list *ui.SliverListController) (transcriptReadingSnapshot, []transcriptReadingSection) {
	if scroll == nil || list == nil || !scroll.Attached() || !list.Attached() {
		return transcriptReadingSnapshot{}, nil
	}
	presentation := presentTranscript(messages)
	metrics := scroll.Metrics()
	for index := len(presentation.Items) - 1; index >= 0; index-- {
		item := presentation.Items[index]
		if item.Kind != transcriptDisplayAssistantProse || item.Item == nil {
			continue
		}
		start, ok := list.OffsetForIndex(index)
		if !ok {
			continue
		}
		end := metrics.ContentHeight
		if next, nextOK := list.OffsetForIndex(index + 1); nextOK && next > start {
			end = next
		}
		if end-start <= metrics.ViewportHeight || end <= metrics.ScrollOffset || start >= metrics.ScrollOffset+metrics.ViewportHeight {
			continue
		}
		document := parseMarkdownDocument(markdownView{Source: assistantProse(item.Item.Message)})
		sections := transcriptReadingSections(document)
		if len(sections) == 0 {
			continue
		}
		offsets := markdownBlockRowOffsets(document)
		sectionOffsets := make([]int, len(sections))
		for sectionIndex, section := range sections {
			sectionOffsets[sectionIndex] = start + 1
			if section.Block < len(offsets) {
				sectionOffsets[sectionIndex] = start + 1 + offsets[section.Block]
			}
		}
		selected := transcriptReadingSelection(sectionOffsets, metrics.ScrollOffset)
		return transcriptReadingSnapshot{
			Visible: true, Title: sections[selected].Title, Selected: selected, Count: len(sections),
		}, sections
	}
	return transcriptReadingSnapshot{}, nil
}

// markdownBlockRowOffsets estimates each block's row within a rendered message.
func markdownBlockRowOffsets(document kitmarkdown.Document) []int {
	offsets := make([]int, len(document.Blocks))
	rows := 0
	for index, block := range document.Blocks {
		if index > 0 && markdownBlocksNeedGap(document.Blocks[index-1], block) {
			rows++
		}
		offsets[index] = rows
		rows += markdownBlockRows(block)
	}
	return offsets
}

// positionTranscriptArrival begins reading a newly delivered tall response at
// its first row, rather than following its tail. Historical snapshots and
// shorter replies retain the usual bottom-follow behavior.
func (s *appState) positionTranscriptArrival() bool {
	if !s.transcriptVisible || !s.scroll.Attached() || !s.transcriptList.Attached() {
		return false
	}
	id := s.transcriptArrivalID
	s.transcriptArrivalID = ""
	items := s.mainTranscriptPresentation().Items
	proseID := "assistant-prose:" + id
	for index, item := range items {
		if index != len(items)-1 || (item.ID != proseID && !strings.HasPrefix(item.ID, proseID+":ordered:")) {
			continue
		}
		start, ok := s.transcriptList.OffsetForIndex(index)
		if !ok {
			return false
		}
		metrics := s.scroll.Metrics()
		if metrics.ContentHeight-start <= metrics.ViewportHeight {
			return false
		}
		s.transcriptList.ScrollToIndex(index, ui.ScrollAlignStart)
		return true
	}
	return false
}

// moveTranscriptReading jumps one section up or down within the message the
// strip is tracking, and unpins follow so new output stays put.
func (s *appState) moveTranscriptReading(_ ui.EventContext, delta int) {
	target := s.transcriptReading.Selected + delta
	if !s.transcriptReading.Visible || target < 0 || target >= s.transcriptReading.Count {
		return
	}
	s.jumpToTranscriptSection(target)
}

// jumpToTranscriptSection scrolls the tracked message so the section begins at
// the top of the viewport.
func (s *appState) jumpToTranscriptSection(section int) {
	s.transcriptArrivalID = ""
	messages := append(append([]transcriptMessage(nil), s.messages...), s.liveMessages...)
	if jumpTranscriptSection(messages, &s.scroll, &s.transcriptList, section) {
		s.needsScroll = false
		s.scrollPendingLayout = false
		s.observeTranscriptReading()
	}
}

// jumpSubagentReading scrolls one subagent conversation to a section and
// leaves its follow-to-end pass unpinned.
func (s *appState) jumpSubagentReading(conversationID string, section int) {
	conversation := protocol.SubagentConversation{ID: conversationID}
	for _, candidate := range s.subagentConversations {
		if candidate.ID == conversationID {
			conversation = candidate
			break
		}
	}
	messages := subagentPaneMessages(conversation, s.subagentTranscripts[conversationID], s.subagentLive[conversationID])
	if !jumpTranscriptSection(messages, s.subagentScrolls[conversationID], s.subagentTranscriptLists[conversationID], section) {
		return
	}
	if s.subagentScrollToEndID == conversationID {
		s.subagentNeedsScroll = false
		s.subagentPendingLayout = false
		s.subagentScrollToEndID = ""
	}
	s.observeSubagentReading()
}

func jumpTranscriptSection(messages []transcriptMessage, scroll *ui.ScrollController, list *ui.SliverListController, section int) bool {
	if scroll == nil || list == nil || !scroll.Attached() || !list.Attached() {
		return false
	}
	presentation := presentTranscript(messages)
	metrics := scroll.Metrics()
	for index := len(presentation.Items) - 1; index >= 0; index-- {
		item := presentation.Items[index]
		if item.Kind != transcriptDisplayAssistantProse || item.Item == nil {
			continue
		}
		start, ok := list.OffsetForIndex(index)
		if !ok {
			continue
		}
		end := metrics.ContentHeight
		if next, nextOK := list.OffsetForIndex(index + 1); nextOK && next > start {
			end = next
		}
		if end-start <= metrics.ViewportHeight {
			continue
		}
		document := parseMarkdownDocument(markdownView{Source: assistantProse(item.Item.Message)})
		sections := transcriptReadingSections(document)
		if section < 0 || section >= len(sections) {
			return false
		}
		offsets := markdownBlockRowOffsets(document)
		row := start + 1
		if sections[section].Block < len(offsets) {
			row = start + 1 + offsets[sections[section].Block]
		}
		scroll.ScrollToOffset(row)
		return true
	}
	return false
}

// transcriptReadingChrome is the strip and in-place picker for one transcript.
func transcriptReadingChrome(reading transcriptReadingSnapshot, pickerOpen bool, sections []transcriptReadingSection, selected int, onOpen ui.VoidCallback, onMove func(ui.EventContext, int), onSelect func(ui.EventContext, int)) []ui.Widget {
	if !reading.Visible {
		return nil
	}
	children := []ui.Widget{ui.Align{Alignment: ui.TopCenter, Child: transcriptReadingStrip{
		Reading: reading, OnOpen: onOpen, OnMove: onMove,
	}}}
	if pickerOpen && len(sections) > 0 {
		titles := make([]string, len(sections))
		for index, section := range sections {
			titles[index] = section.Title
		}
		children = append(children, ui.Positioned{Top: 0, Left: transcriptMinMargin, Child: transcriptReadingPicker{
			Titles: titles, Selected: selected, OnSelect: onSelect, OnClose: onOpen,
		}})
	}
	return children
}

// transcriptReadingPicker lists the tracked message's sections. Choosing one
// jumps to it.
type transcriptReadingPicker struct {
	Titles   []string
	Selected int
	OnSelect func(ui.EventContext, int)
	OnClose  ui.VoidCallback
}

func (w transcriptReadingPicker) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	rows := make([]ui.Widget, 0, len(w.Titles))
	for index, title := range w.Titles {
		style := ui.Style{Foreground: theme.Foreground, Background: theme.Background}
		if index == w.Selected {
			style.Background = theme.SurfaceHovered
		}
		label := fmt.Sprintf("%d  %s", index+1, title)
		target := index
		row := ui.Widget(ui.Text{Value: label, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1})
		if w.OnSelect != nil {
			row = mouseActivator{Child: row, OnPressed: func(ctx ui.EventContext) { w.OnSelect(ctx, target) }}
		}
		rows = append(rows, ui.SizedBox{Height: 1, Child: row})
	}
	width := 0
	for _, title := range w.Titles {
		if label := utf8.RuneCountInString(fmt.Sprintf("%d  %s", len(w.Titles), title)); label > width {
			width = label
		}
	}
	return ui.ConstrainedBox{Constraints: ui.Constraints{MaxWidth: width + 4}, Child: ui.DecoratedBox(ui.Decoration{
		Style:  ui.Style{Background: theme.Background},
		Border: ui.BorderLine(theme.Border),
	}, ui.Padding(ui.All(1), ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch,
		Children: rows,
	}))}
}

// openTranscriptReading toggles the section picker for the tracked message.
func (s *appState) openTranscriptReading(ui.EventContext) {
	if !s.transcriptReading.Visible {
		return
	}
	s.transcriptReadingPickerOpen = !s.transcriptReadingPickerOpen
	if s.transcriptReadingPickerOpen {
		s.subagentReadingPickerID = ""
		s.transcriptReadingPickerSelected = s.transcriptReading.Selected
	}
	s.MarkNeedsBuild()
}

// openSubagentReading toggles the section picker for one conversation tab.
func (s *appState) openSubagentReading(_ ui.EventContext, conversationID string) {
	reading := s.subagentReading[conversationID]
	if !reading.Visible {
		return
	}
	if s.subagentReadingPickerID == conversationID {
		s.subagentReadingPickerID = ""
	} else {
		s.transcriptReadingPickerOpen = false
		s.subagentReadingPickerID = conversationID
		s.subagentReadingPickerSelected = reading.Selected
	}
	s.MarkNeedsBuild()
}

// moveSubagentReading jumps one section within a subagent conversation.
func (s *appState) moveSubagentReading(_ ui.EventContext, conversationID string, delta int) {
	reading := s.subagentReading[conversationID]
	target := reading.Selected + delta
	if !reading.Visible || target < 0 || target >= reading.Count {
		return
	}
	s.jumpSubagentReading(conversationID, target)
}

// moveReadingPicker moves the highlighted section in the open picker.
func (s *appState) moveReadingPicker(delta int) {
	selected := s.transcriptReadingPickerSelected
	count := len(s.transcriptReadingSections)
	if s.subagentReadingPickerID != "" {
		selected = s.subagentReadingPickerSelected
		count = len(s.subagentReadingSections[s.subagentReadingPickerID])
	}
	if count == 0 {
		return
	}
	next := selected + delta
	if next < 0 || next >= count {
		return
	}
	s.SetState(func() {
		if s.subagentReadingPickerID != "" {
			s.subagentReadingPickerSelected = next
			return
		}
		s.transcriptReadingPickerSelected = next
	})
}

// selectTranscriptReading jumps to a section chosen from the open picker.
func (s *appState) selectTranscriptReading(_ ui.EventContext, section int) {
	if conversationID := s.subagentReadingPickerID; conversationID != "" {
		s.SetState(func() { s.subagentReadingPickerID = "" })
		s.jumpSubagentReading(conversationID, section)
		return
	}
	s.SetState(func() { s.transcriptReadingPickerOpen = false })
	s.jumpToTranscriptSection(section)
}
