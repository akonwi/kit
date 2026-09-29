package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	// sessionCWDMaxWidth bounds working directories, keeping their tails.
	sessionCWDMaxWidth   = 40
	sessionIDColumnWidth = 8
)

type sessionExplorerItem struct {
	ID                string
	CWD               string
	Name              string
	UpdatedAt         string
	ParentSessionID   string
	ParentSessionName string
	TreeParentID      string
	Depth             int
	ChildCount        int
	Tree              bool
	Expanded          bool
	MissingParent     bool
	InvalidParent     bool
}

func projectSessionExplorerItems(sessions []protocol.SessionInfo) []sessionExplorerItem {
	items := make([]sessionExplorerItem, len(sessions))
	for index, session := range sessions {
		items[index] = sessionExplorerItem{
			ID: session.ID, CWD: session.CWD, Name: session.Name, UpdatedAt: session.UpdatedAt,
			ParentSessionID: session.ParentSessionID, ParentSessionName: session.ParentSessionName,
		}
	}
	return items
}

type sessionExplorerController struct {
	Open             bool
	Loading          bool
	Error            string
	Switching        bool
	SwitchError      string
	RenameOpen       bool
	RenameSessionID  string
	RenameText       string
	RenameCursorEnd  uint64
	RenamePending    bool
	RenameError      string
	DeleteOpen       bool
	DeleteSessionID  string
	DeletePending    bool
	DeleteError      string
	Query            string
	expanded         map[string]bool
	Sessions         []sessionExplorerItem
	Selection        string
	CurrentSessionID string
	generation       uint64
}

type sessionExplorerSnapshot struct {
	Open            bool
	Loading         bool
	Error           string
	Switching       bool
	SwitchError     string
	RenameOpen      bool
	RenameSessionID string
	RenameText      string
	RenameCursorEnd uint64
	RenamePending   bool
	RenameError     string
	DeleteOpen      bool
	DeleteSessionID string
	DeletePending   bool
	DeleteError     string
	Query           string
	// All is the ordered catalog with remembered expansion; Sessions are the
	// rows the query shows.
	All              []sessionExplorerItem
	Sessions         []sessionExplorerItem
	Selection        string
	CurrentSessionID string
}

func (c *sessionExplorerController) Begin(currentSessionID string) uint64 {
	c.generation++
	c.Open = true
	c.Loading = true
	c.Query = ""
	c.Error = ""
	c.Sessions = nil
	c.Selection = currentSessionID
	c.CurrentSessionID = currentSessionID
	return c.generation
}

// Resolve installs the loaded catalog. A query typed while loading is kept and
// its first match is highlighted.
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
	if c.expanded == nil {
		c.expanded = make(map[string]bool)
	}
	c.expandAncestors(c.CurrentSessionID)
	c.expandAncestors(c.Selection)
	if !pickerQueryBlank(c.Query) {
		c.Selection = c.bestSessionMatch(c.Query)
		return true
	}
	if sessionIndex(c.Sessions, c.Selection) < 0 {
		if sessionIndex(c.Sessions, c.CurrentSessionID) >= 0 {
			c.Selection = c.CurrentSessionID
		} else if len(c.Sessions) > 0 {
			c.Selection = c.Sessions[0].ID
		} else {
			c.Selection = ""
		}
	}
	c.reconcileVisibleSelection()
	return true
}

func (c *sessionExplorerController) Snapshot() sessionExplorerSnapshot {
	return sessionExplorerSnapshot{
		Open: c.Open, Loading: c.Loading, Error: c.Error,
		Switching: c.Switching, SwitchError: c.SwitchError,
		RenameOpen: c.RenameOpen, RenameSessionID: c.RenameSessionID,
		RenameText: c.RenameText, RenameCursorEnd: c.RenameCursorEnd,
		RenamePending: c.RenamePending, RenameError: c.RenameError,
		DeleteOpen: c.DeleteOpen, DeleteSessionID: c.DeleteSessionID,
		DeletePending: c.DeletePending, DeleteError: c.DeleteError,
		Query: c.Query, All: c.treeSessions(), Sessions: c.visibleSessions(), Selection: c.Selection,
		CurrentSessionID: c.CurrentSessionID,
	}
}

func (c *sessionExplorerController) Close() {
	c.generation++
	*c = sessionExplorerController{generation: c.generation, expanded: c.expanded}
}

func (c *sessionExplorerController) Select(sessionID string) {
	if c.Switching || c.RenameOpen || c.DeleteOpen {
		return
	}
	if sessionIndex(c.visibleSessions(), sessionID) >= 0 && c.Selection != sessionID {
		c.Selection = sessionID
		c.SwitchError = ""
		c.DeleteError = ""
	}
}

func (c *sessionExplorerController) ApplyExternalRename(sessionID, name string) bool {
	index := sessionIndex(c.Sessions, sessionID)
	if index < 0 {
		return false
	}
	c.Sessions[index].Name = name
	for i := range c.Sessions {
		if c.Sessions[i].ParentSessionID == sessionID {
			c.Sessions[i].ParentSessionName = name
		}
	}
	c.Sessions = orderedSessions(c.Sessions)
	c.reconcileVisibleSelection()
	return true
}

func (c *sessionExplorerController) BeginRename() bool {
	if _, ok := c.ActivatableSelection(); !ok {
		return false
	}
	index := sessionIndex(c.Sessions, c.Selection)
	c.generation++
	c.RenameOpen = true
	c.RenameSessionID = c.Selection
	c.RenameText = c.Sessions[index].Name
	c.RenameCursorEnd++
	c.RenamePending = false
	c.RenameError = ""
	c.DeleteError = ""
	return true
}

func (c *sessionExplorerController) SetRenameText(value string) {
	if c.RenameOpen && !c.RenamePending {
		c.RenameText = value
		c.RenameError = ""
	}
}

func (c *sessionExplorerController) BeginRenameSave() (uint64, string, string, bool) {
	if !c.RenameOpen || c.RenamePending || sessionIndex(c.Sessions, c.RenameSessionID) < 0 {
		return 0, "", "", false
	}
	name := strings.TrimSpace(c.RenameText)
	if name == "" {
		c.CancelRename()
		return 0, "", "", false
	}
	c.generation++
	c.RenamePending = true
	c.RenameError = ""
	return c.generation, c.RenameSessionID, name, true
}

func (c *sessionExplorerController) ResolveRename(generation uint64, session protocol.SessionInfo, err error) bool {
	if !c.RenameOpen || !c.RenamePending || generation != c.generation {
		return false
	}
	c.RenamePending = false
	if err != nil {
		c.RenameError = err.Error()
		c.RenameCursorEnd++
		return true
	}
	index := sessionIndex(c.Sessions, c.RenameSessionID)
	if index < 0 || session.ID != c.RenameSessionID {
		c.RenameError = "renamed session identity mismatch"
		return true
	}
	c.Sessions[index] = sessionExplorerItem{
		ID: session.ID, CWD: session.CWD, Name: session.Name, UpdatedAt: session.UpdatedAt,
		ParentSessionID: session.ParentSessionID, ParentSessionName: session.ParentSessionName,
	}
	c.ApplyExternalRename(session.ID, session.Name)
	c.Sessions = orderedSessions(c.Sessions)
	c.expandAncestors(c.Selection)
	c.reconcileVisibleSelection()
	c.RenameOpen = false
	c.RenameSessionID = ""
	c.RenameText = ""
	c.RenameError = ""
	return true
}

func (c *sessionExplorerController) CancelRename() {
	if !c.RenameOpen || c.RenamePending {
		return
	}
	c.generation++
	c.RenameOpen = false
	c.RenameSessionID = ""
	c.RenameText = ""
	c.RenameError = ""
}

func (c *sessionExplorerController) BeginDelete() bool {
	if _, ok := c.ActivatableSelection(); !ok {
		return false
	}
	if c.Selection == c.CurrentSessionID {
		c.DeleteError = "Cannot delete the attached session"
		return false
	}
	c.generation++
	c.DeleteOpen = true
	c.DeleteSessionID = c.Selection
	c.DeletePending = false
	c.DeleteError = ""
	return true
}

func (c *sessionExplorerController) BeginDeleteConfirm() (uint64, string, bool) {
	if !c.DeleteOpen || c.DeletePending || sessionIndex(c.Sessions, c.DeleteSessionID) < 0 {
		return 0, "", false
	}
	c.generation++
	c.DeletePending = true
	c.DeleteError = ""
	return c.generation, c.DeleteSessionID, true
}

func (c *sessionExplorerController) ResolveDelete(generation uint64, err error) bool {
	if !c.DeleteOpen || !c.DeletePending || generation != c.generation {
		return false
	}
	c.DeletePending = false
	if err != nil {
		c.DeleteError = err.Error()
		return true
	}
	index := sessionIndex(c.Sessions, c.DeleteSessionID)
	if index < 0 {
		c.DeleteError = "deleted session is no longer listed"
		return true
	}
	visibleIndex := sessionIndex(c.visibleSessions(), c.DeleteSessionID)
	c.Sessions = orderedSessions(append(c.Sessions[:index], c.Sessions[index+1:]...))
	delete(c.expanded, c.DeleteSessionID)
	visible := c.visibleSessions()
	if len(visible) == 0 {
		c.Selection = ""
	} else {
		index = max(0, min(visibleIndex, len(visible)-1))
		c.Selection = visible[index].ID
	}
	c.DeleteOpen = false
	c.DeleteSessionID = ""
	c.DeleteError = ""
	return true
}

func (c *sessionExplorerController) CancelDelete() {
	if !c.DeleteOpen || c.DeletePending {
		return
	}
	c.generation++
	c.DeleteOpen = false
	c.DeleteSessionID = ""
	c.DeleteError = ""
}

func (c *sessionExplorerController) ActivatableSelection() (string, bool) {
	if !c.Open || c.Loading || c.Error != "" || c.Switching || c.RenameOpen || c.DeleteOpen || sessionIndex(c.Sessions, c.Selection) < 0 {
		return "", false
	}
	return c.Selection, true
}

func (c *sessionExplorerController) BeginSwitch() (uint64, string, bool) {
	if !c.Open || c.Loading || c.Error != "" || c.Switching || c.RenameOpen || c.DeleteOpen || sessionIndex(c.Sessions, c.Selection) < 0 {
		return 0, "", false
	}
	c.generation++
	c.Switching = true
	c.SwitchError = ""
	c.DeleteError = ""
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

// Move highlights the visible session delta rows away, wrapping like every
// picker.
func (c *sessionExplorerController) Move(delta int) {
	if !c.Open || c.Loading || c.Error != "" || c.Switching || c.RenameOpen || c.DeleteOpen {
		return
	}
	model := c.keyModel()
	model.Move(c.pickerCatalog(time.Now()), delta)
	c.Select(model.Selection)
}

// HandleKey routes explorer keys through the canonical picker key model from
// controller state, so keys typed before the first paint or before the catalog
// arrives are applied like later ones. Left and Right fold the tree while the
// query is empty; Ctrl+r and Ctrl+d are explorer shortcuts offered only after
// the model leaves a key unhandled.
func (c *sessionExplorerController) HandleKey(key ui.Key) pickerKeyResult {
	if !c.Open || key.EventType == ui.EventRelease {
		return pickerKeyResult{}
	}
	pressed := key.EventType != vaxis.EventPaste
	if c.Switching || c.RenameOpen || c.DeleteOpen {
		if pressed && key.MatchString("Escape") {
			return pickerKeyResult{Handled: true, Dismiss: true}
		}
		return pickerKeyResult{Handled: true}
	}
	if pressed && strings.TrimSpace(c.Query) == "" && (key.MatchString("Left") || key.MatchString("Right")) {
		c.NavigateTree(key.MatchString("Right"))
		return pickerKeyResult{Handled: true}
	}
	model := c.keyModel()
	result := model.HandleKey(key, c.pickerCatalog(time.Now()))
	switch {
	case result.QueryChanged:
		c.SetQuery(model.Query)
	case model.Selection != c.Selection:
		c.Select(model.Selection)
	}
	if result.Handled || !pressed {
		return result
	}
	switch {
	case key.MatchString("Ctrl+r"):
		c.BeginRename()
	case key.MatchString("Ctrl+d"):
		c.BeginDelete()
	default:
		return result
	}
	return pickerKeyResult{Handled: true}
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
	// Activate highlights and opens a clicked session.
	Activate func(ui.EventContext, string)
	// Toggle expands or collapses a clicked disclosure.
	Toggle          func(ui.EventContext, string)
	RenameChanged   ui.TextChangedCallback
	RenameSubmitted ui.TextChangedCallback
}

// sessionExplorerSurface renders the explorer and its rename and delete steps
// in the canonical picker frame. Action names what Enter does to a session.
type sessionExplorerSurface struct {
	Snapshot  sessionExplorerSnapshot
	Callbacks sessionExplorerCallbacks
	Action    string
}

func (w sessionExplorerSurface) Build(ui.BuildContext) ui.Widget {
	if w.Snapshot.RenameOpen {
		return w.renamePrompt()
	}
	return w.picker(time.Now())
}

func (w sessionExplorerSurface) picker(now time.Time) palettePicker {
	snapshot := w.Snapshot
	action := w.Action
	if action == "" {
		action = "switch"
	}
	result := palettePicker{
		Title: "Sessions", Query: snapshot.Query, Search: &pickerSearch{Placeholder: "Search sessions…"},
		Catalog: sessionExplorerPickerItems(snapshot.All, snapshot.CurrentSessionID, now), Filter: filterPickerTree,
		Selection:  snapshot.Selection,
		Footer:     "←→ expand · enter " + action + " · ctrl+r rename · ctrl+d delete",
		OnActivate: w.Callbacks.Activate,
		OnToggle:   w.Callbacks.Toggle,
	}
	// A single session needs no count; the list shows it.
	if len(snapshot.All) > 1 {
		result.TitleMeta = sessionCountLabel(len(snapshot.All))
	}
	switch {
	case snapshot.Loading:
		result.Message, result.MessageTone = "Loading sessions…", pickerToneLoading
		result.Footer = "esc close"
	case snapshot.Error != "":
		result.Message, result.MessageTone = "Could not load sessions: "+snapshot.Error, pickerToneDanger
		result.Footer = "esc close"
	case len(snapshot.Sessions) == 0 && !pickerQueryBlank(snapshot.Query):
		result.Message = "No matching sessions"
	case len(snapshot.Sessions) == 0:
		result.Message = "No sessions"
		result.Footer = "esc close"
	}
	target := sessionExplorerTargetLabel(snapshot.All, snapshot.DeleteSessionID)
	switch {
	case snapshot.Switching:
		result.TitleMeta, result.TitleMetaTone = "switching…", pickerToneLoading
		result.Footer = "esc cancel"
	case snapshot.DeleteOpen && snapshot.DeletePending:
		result.TitleMeta, result.TitleMetaTone = "deleting…", pickerToneLoading
		result.Status, result.Footer = "Deleting \""+target+"\"…", ""
	case snapshot.DeleteOpen && snapshot.DeleteError != "":
		result.Status, result.StatusTone = "Delete failed: "+snapshot.DeleteError+" "+glyphMiddleDot+" enter retry", pickerToneDanger
		result.Footer = "enter retry " + glyphMiddleDot + " esc cancel"
	case snapshot.DeleteOpen:
		result.Status, result.StatusTone = "Delete \""+target+"\"? enter confirm", pickerToneDanger
		result.Footer = "enter confirm " + glyphMiddleDot + " esc cancel"
	case snapshot.SwitchError != "":
		result.Status, result.StatusTone = "Switch failed: "+snapshot.SwitchError+" "+glyphMiddleDot+" enter retry", pickerToneDanger
		result.Footer = "esc close"
	case snapshot.DeleteError != "":
		result.Status, result.StatusTone = snapshot.DeleteError, pickerToneDanger
		result.Footer = "esc close"
	}
	return result
}

func (w sessionExplorerSurface) renamePrompt() palettePickerPrompt {
	snapshot := w.Snapshot
	cursor := len((ui.LayoutContext{}).Characters(snapshot.RenameText))
	prompt := palettePickerPrompt{
		Title: "Rename session", TitleMeta: sessionExplorerTargetLabel(snapshot.All, snapshot.RenameSessionID),
		Input: textInputConfig{
			Value: snapshot.RenameText, Placeholder: "Enter new session name…",
			OnChanged: w.Callbacks.RenameChanged, OnSubmitted: w.Callbacks.RenameSubmitted,
			InitialCursorOffset: &cursor, InitialCursorGeneration: snapshot.RenameCursorEnd, AutoFocus: true,
		},
		Footer: "enter save " + glyphMiddleDot + " esc cancel",
	}
	if snapshot.RenameError != "" {
		prompt.Error = "Rename failed: " + snapshot.RenameError
	}
	if snapshot.RenamePending {
		prompt.TitleMeta, prompt.TitleMetaTone = "saving…", pickerToneLoading
		prompt.Footer = ""
	}
	return prompt
}

func sessionExplorerTargetLabel(sessions []sessionExplorerItem, sessionID string) string {
	if index := sessionIndex(sessions, sessionID); index >= 0 {
		return sessionExplorerItemLabel(sessions[index])
	}
	return shortSessionID(sessionID)
}

// sessionExplorerPickerItems projects visible sessions onto picker rows: the
// working directory is the description, the updated time is the metadata, and
// hierarchy and lineage notes share the hint column.
func sessionExplorerPickerItems(sessions []sessionExplorerItem, currentSessionID string, now time.Time) []pickerItem {
	items := make([]pickerItem, 0, len(sessions))
	for _, session := range sessions {
		item := pickerItem{
			Key: session.ID, Label: sessionExplorerItemLabel(session), Hint: sessionLineageNote(session),
			Description: sessionDisplayCWD(session.CWD, sessionCWDMaxWidth), ParentKey: session.TreeParentID,
			SearchText: sessionDisplayCWD(session.CWD, sessionCWDMaxWidth),
			Meta:       formatSessionUpdated(session.UpdatedAt, now),
			Current:    session.ID == currentSessionID, Depth: session.Depth, ChildCount: session.ChildCount,
		}
		if session.ChildCount > 0 {
			item.Disclosure = pickerDisclosureCollapsed
			if session.Expanded {
				item.Disclosure = pickerDisclosureExpanded
			}
		}
		items = append(items, item)
	}
	return items
}

// sessionLineageNote explains a session whose parent is not shown above it.
func sessionLineageNote(session sessionExplorerItem) string {
	switch {
	case session.InvalidParent:
		return "invalid parent"
	case session.MissingParent:
		parent := strings.TrimSpace(session.ParentSessionName)
		if parent == "" {
			parent = shortSessionID(session.ParentSessionID)
		}
		return "from " + parent
	default:
		return ""
	}
}

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
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
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
