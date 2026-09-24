package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
)

const (
	// MaxFooterItems bounds session-owned footer contributions.
	MaxFooterItems = 64
	// MaxFooterClaims bounds all generation-owned hide claims in one session.
	MaxFooterClaims = 256
	// MaxFooterSegments bounds one item's styled text fragments.
	MaxFooterSegments = 32
	// MaxFooterTextBytes bounds total UTF-8 text in one item.
	MaxFooterTextBytes = 4096
	// FooterLocationID is the only configurable built-in chrome item.
	FooterLocationID = "kit.footer.location"
)

// FooterStyle keeps public semantic tokens unresolved until client rendering.
type FooterStyle struct {
	FG            string
	BG            string
	Bold          bool
	Dim           bool
	Italic        bool
	Underline     bool
	Strikethrough bool
}

// FooterSegment is safe single-line text with semantic styling.
type FooterSegment struct {
	Text  string
	Style FooterStyle
}

// FooterItem is owned by exactly one session-local plugin generation.
type FooterItem struct {
	Owner       InstanceID
	ID, LocalID string
	Content     []FooterSegment
}

// FooterState projects visible items in first-registration order. Updating an
// item preserves its position; removing and setting it again appends it.
type FooterState struct {
	Items          []FooterItem
	LocationHidden bool
}

func validFooterTarget(id string) bool {
	if id == FooterLocationID {
		return true
	}
	plugin, local, ok := strings.Cut(id, ".")
	return ok && pluginID.MatchString(plugin) && plugin != "kit" && !strings.HasPrefix(plugin, "kit-") && localID(local)
}

func parseFooterStyle(raw json.RawMessage) (FooterStyle, error) {
	var style FooterStyle
	fields, err := contributionObject(raw, "fg", "bg", "bold", "dim", "italic", "underline", "strikethrough")
	if err != nil {
		return style, err
	}
	for name, target := range map[string]*string{"fg": &style.FG, "bg": &style.BG} {
		if value, ok := fields[name]; ok {
			if json.Unmarshal(value, target) != nil || !footerThemeTokens[*target] {
				return style, rpcError(-32602, "Invalid footer theme token")
			}
		}
	}
	for name, target := range map[string]*bool{"bold": &style.Bold, "dim": &style.Dim, "italic": &style.Italic, "underline": &style.Underline, "strikethrough": &style.Strikethrough} {
		if value, ok := fields[name]; ok {
			if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, target) != nil {
				return style, rpcError(-32602, "Invalid footer style flag")
			}
		}
	}
	return style, nil
}

func parseFooterItem(raw json.RawMessage) (FooterItem, error) {
	var item FooterItem
	fields, err := contributionObject(raw, "id", "content", "side", "clickable", "action")
	if err != nil {
		return item, err
	}
	if item.LocalID, err = contributionString(fields, "id", 128, true); err != nil {
		return item, err
	}
	if !localID(item.LocalID) {
		return item, rpcError(-32602, "Invalid footer item id")
	}
	if value, ok := fields["side"]; ok {
		var side string
		if json.Unmarshal(value, &side) != nil || (side != "left" && side != "right") {
			return item, rpcError(-32602, "Invalid footer side")
		}
		if side != "right" {
			return item, rpcError(-32601, "Only bottom-right footer contributions are supported")
		}
	}
	if _, ok := fields["action"]; ok {
		return item, rpcError(-32601, "Footer URL actions are not implemented")
	}
	if value, ok := fields["clickable"]; ok {
		var clickable bool
		if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &clickable) != nil {
			return item, rpcError(-32602, "Invalid footer clickable flag")
		}
		if clickable {
			return item, rpcError(-32601, "Footer click callbacks are not implemented")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(fields["content"]))
	if opening, err := decoder.Token(); err != nil || opening != json.Delim('[') {
		return item, rpcError(-32602, "Footer content must be an array")
	}
	total := 0
	for decoder.More() {
		if len(item.Content) >= MaxFooterSegments {
			return item, rpcError(-32005, "Too many footer segments")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return item, rpcError(-32602, "Invalid footer segment")
		}
		segmentFields, err := contributionObject(raw, "text", "style")
		if err != nil {
			return item, err
		}
		var segment FooterSegment
		text, ok := segmentFields["text"]
		if !ok || bytes.Equal(text, []byte("null")) || json.Unmarshal(text, &segment.Text) != nil || !contributionText(segment.Text, MaxFooterTextBytes, false) {
			return item, rpcError(-32602, "Invalid footer text")
		}
		total += len(segment.Text)
		if total > MaxFooterTextBytes {
			return item, rpcError(-32005, "Footer text limit exceeded")
		}
		if style, ok := segmentFields["style"]; ok {
			segment.Style, err = parseFooterStyle(style)
			if err != nil {
				return item, err
			}
		}
		item.Content = append(item.Content, segment)
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') || len(item.Content) == 0 {
		return item, rpcError(-32602, "Footer content must contain segments")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return item, rpcError(-32602, "Trailing footer content")
	}
	return item, nil
}

func (h *Host) handleFooter(ctx context.Context, owner InstanceID, method string, params json.RawMessage) (json.RawMessage, error) {
	var item FooterItem
	var id string
	var err error
	switch method {
	case "kit/footer/set":
		item, err = parseFooterItem(params)
	case "kit/footer/clear":
		id, err = parseLocalID(params)
	case "kit/footer/hide", "kit/footer/show":
		var fields map[string]json.RawMessage
		fields, err = contributionObject(params, "id")
		if err == nil {
			id, err = contributionString(fields, "id", 257, true)
		}
		if err == nil && !validFooterTarget(id) {
			err = rpcError(-32602, "Target is not configurable bottom-right footer content")
		}
	default:
		return nil, rpcError(-32601, "Unsupported footer operation")
	}
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if h.activeEntryLocked(owner) == nil {
		return nil, rpcError(-32002, "Plugin generation is unavailable")
	}
	h.pruneFooterLocked()
	switch method {
	case "kit/footer/set":
		item.Owner = owner
		item.ID = owner.PluginID + "." + item.LocalID
		index := -1
		for i, old := range h.footerItems {
			if old.ID == item.ID {
				index = i
				break
			}
		}
		if index >= 0 {
			h.footerItems[index] = item
		} else {
			if len(h.footerItems) >= MaxFooterItems {
				return nil, rpcError(-32005, "Session footer item limit exceeded")
			}
			h.footerItems = append(h.footerItems, item)
		}
		h.publishChange()
		return json.Marshal(struct {
			ID string `json:"id"`
		}{item.ID})
	case "kit/footer/clear":
		for i, old := range h.footerItems {
			if old.Owner == owner && old.LocalID == id {
				h.footerItems = append(h.footerItems[:i], h.footerItems[i+1:]...)
				h.publishChange()
				break
			}
		}
	case "kit/footer/hide":
		if _, exists := h.footerClaims[owner][id]; exists {
			return json.RawMessage("null"), nil
		}
		total := 0
		for _, claims := range h.footerClaims {
			total += len(claims)
		}
		if total >= MaxFooterClaims {
			return nil, rpcError(-32005, "Session footer hide-claim limit exceeded")
		}
		if h.footerClaims == nil {
			h.footerClaims = make(map[InstanceID]map[string]struct{})
		}
		if h.footerClaims[owner] == nil {
			h.footerClaims[owner] = make(map[string]struct{})
		}
		h.footerClaims[owner][id] = struct{}{}
		h.publishChange()
	case "kit/footer/show":
		delete(h.footerClaims[owner], id)
		if len(h.footerClaims[owner]) == 0 {
			delete(h.footerClaims, owner)
		}
		h.publishChange()
	}
	return json.RawMessage("null"), nil
}

func (h *Host) pruneFooterLocked() {
	items := h.footerItems[:0]
	for _, item := range h.footerItems {
		if h.activeEntryLocked(item.Owner) != nil {
			items = append(items, item)
		}
	}
	clear(h.footerItems[len(items):])
	h.footerItems = items
	for owner := range h.footerClaims {
		if h.activeEntryLocked(owner) == nil {
			delete(h.footerClaims, owner)
		}
	}
}

// Footer excludes revoked instances atomically and returns detached content.
func (h *Host) Footer() FooterState {
	h.mu.Lock()
	defer h.mu.Unlock()
	var result FooterState
	owners := make(map[InstanceID]bool)
	active := func(owner InstanceID) bool {
		if value, known := owners[owner]; known {
			return value
		}
		entry := h.activeEntryLocked(owner)
		owners[owner] = entry != nil && entry.instance != nil
		return owners[owner]
	}
	hidden := make(map[string]bool)
	for owner, claims := range h.footerClaims {
		if active(owner) {
			for id := range claims {
				hidden[id] = true
			}
		}
	}
	result.LocationHidden = hidden[FooterLocationID]
	for _, item := range h.footerItems {
		if active(item.Owner) && !hidden[item.ID] {
			item.Content = append([]FooterSegment(nil), item.Content...)
			result.Items = append(result.Items, item)
		}
	}
	return result
}

// Public v1 tokens are validated without resolving client-specific colors.
var footerThemeTokens = map[string]bool{
	"bg":                         true,
	"bgSurface":                  true,
	"bgMuted":                    true,
	"bgAccent":                   true,
	"bgTransparent":              true,
	"borderDefault":              true,
	"borderFocused":              true,
	"borderAccent":               true,
	"borderDebug":                true,
	"borderStatus":               true,
	"composerBashBorder":         true,
	"composerBashExcludedBorder": true,
	"textPrimary":                true,
	"textSecondary":              true,
	"textMuted":                  true,
	"textPlaceholder":            true,
	"textDebug":                  true,
	"userText":                   true,
	"userTextFocused":            true,
	"userBorder":                 true,
	"assistantText":              true,
	"toolText":                   true,
	"reviewText":                 true,
	"errorText":                  true,
	"warningText":                true,
	"subagentText":               true,
	"debugLabel":                 true,
	"metaText":                   true,
	"attachmentText":             true,
	"cursor":                     true,
	"pickerBg":                   true,
	"pickerBorder":               true,
	"pickerFocusedBg":            true,
	"pickerFocusedText":          true,
	"pickerItemText":             true,
	"pickerScrollThumb":          true,
	"pickerScrollTrack":          true,
	"scrollbarFg":                true,
	"scrollbarBg":                true,
	"panelText":                  true,
	"progressNormal":             true,
	"progressWarning":            true,
	"progressCritical":           true,
	"toggleOn":                   true,
	"diffAddedBg":                true,
	"diffRemovedBg":              true,
	"diffAddedContentBg":         true,
	"diffRemovedContentBg":       true,
	"diffAddedLineNumberBg":      true,
	"diffRemovedLineNumberBg":    true,
	"diffCursorBg":               true,
	"diffCursorGutterBg":         true,
	"diffCursorAddedBg":          true,
	"diffCursorRemovedBg":        true,
}
