package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	sessionExplorerMaxVisible = 14
	sessionUpdatedColumnWidth = 10
	sessionCWDCompactWidth    = 32
	sessionCWDExpandedWidth   = 40
	sessionIDColumnWidth      = 8
	sessionExplorerChromeRows = 6
)

type sessionExplorerItem struct {
	ID        string
	CWD       string
	Name      string
	UpdatedAt string
}

func projectSessionExplorerItems(sessions []protocol.SessionInfo) []sessionExplorerItem {
	items := make([]sessionExplorerItem, len(sessions))
	for index, session := range sessions {
		items[index] = sessionExplorerItem{
			ID: session.ID, CWD: session.CWD, Name: session.Name, UpdatedAt: session.UpdatedAt,
		}
	}
	return items
}

type sessionExplorerController struct {
	Open                bool
	Loading             bool
	Error               string
	Switching           bool
	SwitchError         string
	Sessions            []sessionExplorerItem
	Selection           string
	CurrentSessionID    string
	scroll              ui.ScrollController
	list                ui.SliverListController
	layout              pickerDialogLayoutState
	viewportRows        int
	needsReveal         bool
	revealPendingLayout bool
	generation          uint64
}

type sessionExplorerSnapshot struct {
	Open             bool
	Loading          bool
	Error            string
	Switching        bool
	SwitchError      string
	Sessions         []sessionExplorerItem
	Selection        string
	CurrentSessionID string
	Scroll           *ui.ScrollController
	List             *ui.SliverListController
	Layout           *pickerDialogLayoutState
}

func (c *sessionExplorerController) Begin(currentSessionID string) uint64 {
	c.generation++
	c.Open = true
	c.Loading = true
	c.Error = ""
	c.Sessions = nil
	c.Selection = currentSessionID
	c.CurrentSessionID = currentSessionID
	c.scroll = ui.ScrollController{}
	c.list = ui.SliverListController{}
	c.layout = pickerDialogLayoutState{}
	c.viewportRows = 0
	c.needsReveal = false
	c.revealPendingLayout = false
	return c.generation
}

func (c *sessionExplorerController) Resolve(generation uint64, sessions []sessionExplorerItem, err error) bool {
	if !c.Open || generation != c.generation {
		return false
	}
	c.Loading = false
	if err != nil {
		c.Error = err.Error()
		c.Sessions = nil
		return true
	}
	c.Error = ""
	c.Sessions = orderedSessions(sessions)
	if sessionIndex(c.Sessions, c.Selection) < 0 {
		if sessionIndex(c.Sessions, c.CurrentSessionID) >= 0 {
			c.Selection = c.CurrentSessionID
		} else if len(c.Sessions) > 0 {
			c.Selection = c.Sessions[0].ID
		} else {
			c.Selection = ""
		}
	}
	c.requestReveal()
	return true
}

func (c *sessionExplorerController) Snapshot() sessionExplorerSnapshot {
	return sessionExplorerSnapshot{
		Open: c.Open, Loading: c.Loading, Error: c.Error,
		Switching: c.Switching, SwitchError: c.SwitchError,
		Sessions: append([]sessionExplorerItem(nil), c.Sessions...), Selection: c.Selection,
		CurrentSessionID: c.CurrentSessionID, Scroll: &c.scroll, List: &c.list, Layout: &c.layout,
	}
}

func (c *sessionExplorerController) Close() {
	c.generation++
	*c = sessionExplorerController{generation: c.generation}
}

func (c *sessionExplorerController) Select(sessionID string) {
	if c.Switching {
		return
	}
	if sessionIndex(c.Sessions, sessionID) >= 0 && c.Selection != sessionID {
		c.Selection = sessionID
		c.SwitchError = ""
		c.requestReveal()
	}
}

func (c *sessionExplorerController) ActivatableSelection() (string, bool) {
	if !c.Open || c.Loading || c.Error != "" || c.Switching || sessionIndex(c.Sessions, c.Selection) < 0 {
		return "", false
	}
	return c.Selection, true
}

func (c *sessionExplorerController) BeginSwitch() (uint64, string, bool) {
	if !c.Open || c.Loading || c.Error != "" || c.Switching || sessionIndex(c.Sessions, c.Selection) < 0 {
		return 0, "", false
	}
	c.generation++
	c.Switching = true
	c.SwitchError = ""
	return c.generation, c.Selection, true
}

func (c *sessionExplorerController) CancelSwitch() {
	if !c.Switching {
		return
	}
	c.generation++
	c.Switching = false
	c.SwitchError = ""
}

func (c *sessionExplorerController) ResolveSwitch(generation uint64, err error) bool {
	if !c.Open || !c.Switching || generation != c.generation {
		return false
	}
	c.Switching = false
	if err != nil {
		c.SwitchError = err.Error()
		return true
	}
	c.Close()
	return true
}

func (c *sessionExplorerController) Move(delta int) {
	if !c.Open || c.Loading || c.Error != "" || c.Switching || len(c.Sessions) == 0 || delta == 0 {
		return
	}
	index := sessionIndex(c.Sessions, c.Selection)
	if index < 0 {
		index = 0
	}
	index = max(0, min(len(c.Sessions)-1, index+delta))
	if next := c.Sessions[index].ID; next != c.Selection {
		c.Selection = next
		c.SwitchError = ""
		c.requestReveal()
	}
}

func (c *sessionExplorerController) requestReveal() {
	if c.Selection == "" {
		return
	}
	c.needsReveal = true
	c.revealPendingLayout = true
}

func (c *sessionExplorerController) TickFrame() bool {
	if !c.Open {
		return false
	}
	if c.layout.AvailableRows <= 0 {
		c.viewportRows = 0
		c.needsReveal = c.Selection != ""
		return false
	}
	if !c.scroll.Attached() {
		return c.needsReveal
	}
	metrics := c.scroll.Metrics()
	viewportRows := metrics.ViewportHeight
	if viewportRows <= 0 {
		c.viewportRows = viewportRows
		return false
	}
	if viewportRows != c.viewportRows {
		c.viewportRows = viewportRows
		c.needsReveal = c.Selection != ""
	}
	if !c.needsReveal {
		return false
	}
	if c.revealPendingLayout {
		c.revealPendingLayout = false
		return true
	}
	if !c.list.Attached() {
		return true
	}
	first, last, rangeAvailable := c.list.VisibleRange()
	index := sessionIndex(c.Sessions, c.Selection)
	if index < 0 {
		c.needsReveal = false
		return false
	}
	c.list.ScrollToIndex(index, ui.ScrollAlignCenter)
	first, last, rangeAvailable = c.list.VisibleRange()
	if rangeAvailable && index >= first && index < last {
		c.needsReveal = false
		return false
	}
	return true
}

// HandleKey updates navigation from controller state so rapid input does not
// depend on whether the newly opened dialog has painted yet.
func (c *sessionExplorerController) HandleKey(key ui.Key) bool {
	if !c.Open || key.EventType == ui.EventRelease {
		return false
	}
	if key.EventType == vaxis.EventPaste {
		return true
	}
	if key.MatchString("Escape") || key.MatchString("Ctrl+c") {
		return false
	}
	switch {
	case key.MatchString("Up"), key.MatchString("k"):
		c.Move(-1)
	case key.MatchString("Down"), key.MatchString("j"):
		c.Move(1)
	case key.MatchString("Page_Up"):
		c.Move(-sessionExplorerMaxVisible)
	case key.MatchString("Page_Down"):
		c.Move(sessionExplorerMaxVisible)
	}
	return true
}

func orderedSessions(sessions []sessionExplorerItem) []sessionExplorerItem {
	ordered := append([]sessionExplorerItem(nil), sessions...)
	sort.SliceStable(ordered, func(left, right int) bool {
		leftTime := sessionTimestamp(ordered[left].UpdatedAt)
		rightTime := sessionTimestamp(ordered[right].UpdatedAt)
		if leftTime.Equal(rightTime) {
			return ordered[left].ID < ordered[right].ID
		}
		return leftTime.After(rightTime)
	})
	return ordered
}

func sessionTimestamp(raw string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, raw)
	return parsed
}

func sessionIndex(sessions []sessionExplorerItem, sessionID string) int {
	for index, session := range sessions {
		if session.ID == sessionID {
			return index
		}
	}
	return -1
}

type sessionExplorerCallbacks struct {
	Select func(ui.EventContext, string)
}

type sessionExplorerSurface struct {
	Snapshot  sessionExplorerSnapshot
	Callbacks sessionExplorerCallbacks
}

func (w sessionExplorerSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	meta := ""
	metaStyle := ui.Style{Foreground: theme.MutedForeground}
	switch {
	case w.Snapshot.Loading:
		meta = "Loading…"
	case w.Snapshot.Error != "":
		meta = "Unavailable"
	case w.Snapshot.Switching:
		meta = "Switching…"
	case w.Snapshot.SwitchError != "":
		meta = "Switch failed"
		metaStyle = ui.Style{Foreground: theme.DangerText}
	case len(w.Snapshot.Sessions) > 1:
		meta = sessionCountLabel(len(w.Snapshot.Sessions))
	}
	headerChildren := []ui.Widget{
		ui.Expanded(ui.Text{Value: "Session Explorer", Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
	}
	if w.Snapshot.Switching {
		headerChildren = append(headerChildren,
			spinner{Style: metaStyle}, ui.SizedBox{Width: 1},
			ui.Text{Value: meta, Style: metaStyle, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		)
	} else if meta != "" {
		headerChildren = append(headerChildren, ui.Text{
			Value: meta, Style: metaStyle,
			Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		})
	}
	header := ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: headerChildren}
	mutedStyle := ui.Style{Foreground: theme.MutedForeground}
	var footer ui.Widget = ui.Text{
		Value: "↑↓ move · page up/down · enter switch · esc close", Style: mutedStyle,
		Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
	}
	switch {
	case w.Snapshot.Switching:
		footer = ui.Text{Value: "esc cancel", Style: mutedStyle, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}
	case w.Snapshot.SwitchError != "":
		footer = ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			ui.Expanded(ui.Text{
				Value: "Switch failed: " + w.Snapshot.SwitchError, Style: ui.Style{Foreground: theme.DangerText},
				Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
			}),
			ui.SizedBox{Width: 2},
			ui.Text{Value: "enter retry · esc close", Style: mutedStyle, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		}}
	case w.Snapshot.Loading || w.Snapshot.Error != "" || len(w.Snapshot.Sessions) == 0:
		footer = ui.Text{Value: "esc close", Style: mutedStyle, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}
	}
	scroll := w.Snapshot.Scroll
	if scroll == nil {
		scroll = &ui.ScrollController{}
	}
	list := w.Snapshot.List
	if list == nil {
		list = &ui.SliverListController{}
	}
	layout := w.Snapshot.Layout
	if layout == nil {
		layout = &pickerDialogLayoutState{}
	}
	body := ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, header),
		ui.SizedBox{Height: 1},
		ui.Expanded(sessionExplorerBodyClip{Layout: layout, Child: ui.Padding(
			ui.Insets{Right: 2, Left: 2}, w.body(theme, scroll, list, layout),
		)}),
	}}
	content := pickerDialogContent(theme, body, footer)
	return pickerDialogPositioner{
		Percent: 85, MinWidth: 44, MaxWidth: 120, Height: pickerModalMinHeight,
		ReservedRows: sessionExplorerChromeRows, State: layout, Child: content,
	}
}

func (w sessionExplorerSurface) body(theme ui.Theme, scroll *ui.ScrollController, list *ui.SliverListController, layout *pickerDialogLayoutState) ui.Widget {
	if w.Snapshot.Loading {
		return sessionExplorerStateBody(spinnerWithLabel("Loading sessions…", ui.Style{Foreground: theme.MutedForeground}))
	}
	if w.Snapshot.Error != "" {
		return sessionExplorerStateBody(ui.Text{Value: "Could not load sessions.", Style: ui.Style{Foreground: theme.DangerText}})
	}
	if len(w.Snapshot.Sessions) == 0 {
		return sessionExplorerStateBody(ui.Text{Value: "No sessions", Style: ui.Style{Foreground: theme.MutedForeground}})
	}
	return ui.Scrollbar{Child: ui.CustomScrollView{
		Controller: scroll,
		Slivers: []ui.Widget{ui.SliverListBuilder{
			Controller: list, Count: len(w.Snapshot.Sessions), ItemExtent: 1, Overscan: 2,
			Builder: func(_ ui.BuildContext, index int) ui.Widget {
				session := w.Snapshot.Sessions[index]
				return sessionExplorerRow{
					Session: session, Current: session.ID == w.Snapshot.CurrentSessionID,
					Selected:     session.ID == w.Snapshot.Selection,
					SessionCount: len(w.Snapshot.Sessions), Layout: layout,
					OnPressed: func(event ui.EventContext) {
						if w.Callbacks.Select != nil {
							w.Callbacks.Select(event, session.ID)
						}
					},
				}
			},
		}},
	}}
}

func sessionExplorerStateBody(child ui.Widget) ui.Widget { return ui.Center(child) }

type sessionExplorerBodyClip struct {
	Layout *pickerDialogLayoutState
	Child  ui.Widget
}

func (w sessionExplorerBodyClip) WidgetChild() ui.Widget { return w.Child }

func (w sessionExplorerBodyClip) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderSessionExplorerBodyClip{State: w.Layout}
}

func (w sessionExplorerBodyClip) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	object.(*renderSessionExplorerBodyClip).State = w.Layout
}

type renderSessionExplorerBodyClip struct {
	ui.SingleChildRenderObject
	State *pickerDialogLayoutState
}

func (r *renderSessionExplorerBodyClip) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	if r.State != nil && r.State.AvailableRows <= 0 {
		r.SetSize(ui.Size{Width: sessionExplorerBodyWidth(constraints)})
		return
	}
	if child := r.Child(); child != nil {
		child.Layout(ctx, constraints)
	}
	r.SetSize(constraints.Constrain(ui.Size{Width: constraints.MaxWidth, Height: constraints.MaxHeight}))
}

func (r *renderSessionExplorerBodyClip) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	if r.State != nil && r.State.AvailableRows <= 0 {
		return ui.Size{Width: sessionExplorerBodyWidth(constraints)}
	}
	return constraints.Constrain(ui.Size{Width: constraints.MaxWidth, Height: constraints.MaxHeight})
}

func sessionExplorerBodyWidth(constraints ui.Constraints) int {
	if constraints.HasBoundedWidth() {
		return constraints.MaxWidth
	}
	return constraints.MinWidth
}

func (r *renderSessionExplorerBodyClip) Paint(painter *ui.Painter, offset ui.Offset) {
	if r.State != nil && r.State.AvailableRows <= 0 {
		return
	}
	if child := r.Child(); child != nil {
		child.Paint(painter, offset)
	}
}

func (*renderSessionExplorerBodyClip) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

func sessionCountLabel(count int) string {
	if count == 1 {
		return "1 session"
	}
	return strconv.Itoa(count) + " sessions"
}

func formatSessionUpdated(raw string, now time.Time) string {
	updated := sessionTimestamp(raw)
	if updated.IsZero() {
		return "unknown"
	}
	age := now.Sub(updated)
	if age < 0 {
		age = 0
	}
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return strconv.Itoa(int(age/time.Minute)) + "m ago"
	case age < 24*time.Hour:
		return strconv.Itoa(int(age/time.Hour)) + "h ago"
	case age < 30*24*time.Hour:
		return strconv.Itoa(int(age/(24*time.Hour))) + "d ago"
	default:
		return updated.Format("2006-01-02")
	}
}

func sessionDisplayCWD(cwd string, width int) string {
	display := filepath.Clean(cwd)
	if home, err := os.UserHomeDir(); err == nil {
		if relative, err := filepath.Rel(home, display); err == nil {
			switch {
			case relative == ".":
				display = "~"
			case relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)):
				display = filepath.Join("~", relative)
			}
		}
	}
	return truncateStartCells(display, width)
}

func truncateStartCells(value string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	characters := (ui.LayoutContext{}).Characters(value)
	width := 0
	for _, character := range characters {
		width += character.Width
	}
	if width <= maximum {
		return value
	}
	remaining := maximum - 1
	start := len(characters)
	for start > 0 && characters[start-1].Width <= remaining {
		start--
		remaining -= characters[start].Width
	}
	var suffix strings.Builder
	for _, character := range characters[start:] {
		suffix.WriteString(character.Grapheme)
	}
	return glyphEllipsis + suffix.String()
}

func sessionExplorerItemLabel(session sessionExplorerItem) string {
	if name := strings.TrimSpace(session.Name); name != "" {
		return name
	}
	return shortSessionID(session.ID)
}

func shortSessionID(id string) string {
	id = strings.TrimPrefix(id, "session_")
	runes := []rune(id)
	if len(runes) > sessionIDColumnWidth {
		return string(runes[:sessionIDColumnWidth])
	}
	return id
}

type sessionExplorerRow struct {
	Session      sessionExplorerItem
	Current      bool
	Selected     bool
	SessionCount int
	Layout       *pickerDialogLayoutState
	OnPressed    ui.VoidCallback
}

func (w sessionExplorerRow) WidgetKey() ui.KeyValue {
	return ui.KeyValue("session-explorer-row:" + w.Session.ID)
}

func (sessionExplorerRow) CreateState() ui.State { return &sessionExplorerRowState{} }

type sessionExplorerRowState struct {
	ui.StateBase
	hovered bool
}

func (s *sessionExplorerRowState) Build(ctx ui.BuildContext) ui.Widget {
	row := s.Widget().(sessionExplorerRow)
	theme := ui.MustDepend[ui.Theme](ctx)
	background := theme.Background
	primary := theme.Foreground
	secondary := theme.MutedForeground
	if row.Current {
		primary = theme.PrimaryText
	}
	if row.Selected {
		background = theme.Foreground
		primary = theme.Background
		secondary = theme.Background
	} else if s.hovered {
		background = theme.SurfaceHovered
	}
	marker := "  "
	if row.Current {
		marker = glyphCheck + " "
	}
	primaryStyle := ui.Style{Foreground: primary, Background: background}
	secondaryStyle := ui.Style{Foreground: secondary, Background: background}
	shortID := ""
	if strings.TrimSpace(row.Session.Name) != "" {
		shortID = shortSessionID(row.Session.ID)
	}
	content := sessionExplorerRowLayout{SessionCount: row.SessionCount, Layout: row.Layout, Children: []ui.Widget{
		ui.Text{Value: marker + sessionExplorerItemLabel(row.Session), Style: primaryStyle, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		ui.Text{Value: formatSessionUpdated(row.Session.UpdatedAt, time.Now()), Style: secondaryStyle, Align: ui.TextAlignRight, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		ui.Text{Value: sessionDisplayCWD(row.Session.CWD, sessionCWDCompactWidth), Style: secondaryStyle, Align: ui.TextAlignRight, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		ui.Text{Value: sessionDisplayCWD(row.Session.CWD, sessionCWDExpandedWidth), Style: secondaryStyle, Align: ui.TextAlignRight, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		ui.Text{Value: shortID, Style: secondaryStyle, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
	}}
	return mouseActivator{
		OnPressed: row.OnPressed,
		OnHover: func(ui.EventContext) {
			if !s.hovered {
				s.SetState(func() { s.hovered = true })
			}
		},
		OnHoverExit: func(ui.EventContext) {
			if s.hovered {
				s.SetState(func() { s.hovered = false })
			}
		},
		Child: ui.SizedBox{Height: 1, Child: ui.DecoratedBox(
			ui.Decoration{Style: ui.Style{Background: background}}, content,
		)},
	}
}

type sessionExplorerRowLayout struct {
	SessionCount int
	Layout       *pickerDialogLayoutState
	Children     []ui.Widget
}

func (w sessionExplorerRowLayout) WidgetChildren() []ui.Widget { return w.Children }

func (w sessionExplorerRowLayout) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderSessionExplorerRowLayout{SessionCount: w.SessionCount, State: w.Layout}
}

func (w sessionExplorerRowLayout) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderSessionExplorerRowLayout)
	if render.SessionCount == w.SessionCount && render.State == w.Layout {
		return
	}
	render.SessionCount = w.SessionCount
	render.State = w.Layout
	render.MarkNeedsLayout()
}

type sessionExplorerRowParentData struct {
	Offset  ui.Offset
	Visible bool
}

func (data sessionExplorerRowParentData) RenderOffset() ui.Offset { return data.Offset }

type renderSessionExplorerRowLayout struct {
	ui.MultiChildRenderObject
	SessionCount int
	State        *pickerDialogLayoutState
}

func (r *renderSessionExplorerRowLayout) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	r.layoutChildren(ctx, constraints, false)
}

func (r *renderSessionExplorerRowLayout) DryLayout(ctx ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return r.layoutChildren(ctx, constraints, true)
}

func (r *renderSessionExplorerRowLayout) layoutChildren(ctx ui.LayoutContext, constraints ui.Constraints, dry bool) ui.Size {
	width := constraints.MinWidth
	if constraints.HasBoundedWidth() {
		width = constraints.MaxWidth
	}
	contentWidth := width
	if r.State != nil && r.SessionCount > max(1, r.State.AvailableRows) {
		contentWidth = max(0, contentWidth-1)
	}
	showUpdated := contentWidth >= 40
	showCWD := contentWidth >= 68
	showExpandedCWD := showCWD && contentWidth >= 76
	showID := contentWidth >= 104
	columnWidths := []int{0, 0, 0, 0, 0}
	visible := []bool{true, showUpdated, showCWD && !showExpandedCWD, showExpandedCWD, showID}
	if showUpdated {
		columnWidths[1] = sessionUpdatedColumnWidth
	}
	if showCWD && !showExpandedCWD {
		columnWidths[2] = sessionCWDCompactWidth
	}
	if showExpandedCWD {
		columnWidths[3] = sessionCWDExpandedWidth
	}
	if showID {
		columnWidths[4] = sessionIDColumnWidth
	}
	metadataWidth := 0
	visibleMetadata := 0
	for index := 1; index < len(columnWidths); index++ {
		if visible[index] {
			metadataWidth += columnWidths[index]
			visibleMetadata++
		}
	}
	columnWidths[0] = max(0, contentWidth-metadataWidth-visibleMetadata)
	x := 0
	children := r.Children()
	for index, child := range children {
		isVisible := index < len(visible) && visible[index]
		childWidth := 0
		if index < len(columnWidths) {
			childWidth = columnWidths[index]
		}
		childConstraints := ui.Tight(ui.Size{Width: childWidth, Height: 1})
		if dry {
			ui.DryLayout(ctx, child, childConstraints)
		} else {
			child.Layout(ctx, childConstraints)
			child.Base().SetParentData(sessionExplorerRowParentData{Offset: ui.Offset{X: x}, Visible: isVisible})
		}
		if isVisible {
			x += childWidth + 1
		}
	}
	return constraints.Constrain(ui.Size{Width: width, Height: 1})
}

func (r *renderSessionExplorerRowLayout) Paint(painter *ui.Painter, offset ui.Offset) {
	for _, child := range r.Children() {
		data, _ := child.Base().ParentData().(sessionExplorerRowParentData)
		if data.Visible {
			child.Paint(painter, offset.Add(data.Offset))
		}
	}
}

func (r *renderSessionExplorerRowLayout) ChildOffset(child ui.RenderObject) ui.Offset {
	data, _ := child.Base().ParentData().(sessionExplorerRowParentData)
	return data.Offset
}

func (*renderSessionExplorerRowLayout) HitTest(*ui.HitTestResult, ui.Point) bool { return false }
