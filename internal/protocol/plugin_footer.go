package protocol

import (
	"errors"
	"github.com/akonwi/kit/internal/theme"
	"strings"
)

// PluginFooterStyle retains renderer-neutral semantic token styles.
type PluginFooterStyle struct {
	FG            string `json:"fg,omitempty"`
	BG            string `json:"bg,omitempty"`
	Bold          bool   `json:"bold,omitempty"`
	Dim           bool   `json:"dim,omitempty"`
	Italic        bool   `json:"italic,omitempty"`
	Underline     bool   `json:"underline,omitempty"`
	Strikethrough bool   `json:"strikethrough,omitempty"`
}

// PluginFooterSegment is one styled run of footer text.
type PluginFooterSegment struct {
	Text  string            `json:"text"`
	Style PluginFooterStyle `json:"style"`
}

// PluginFooterItem is a generation-owned bottom-right contribution.
type PluginFooterItem struct {
	ID       string                `json:"id"`
	PluginID string                `json:"pluginId"`
	Instance string                `json:"instance"`
	Content  []PluginFooterSegment `json:"content"`
}

// PluginFooter projects visible ordered items and aggregate location hiding.
type PluginFooter struct {
	Items          []PluginFooterItem `json:"items"`
	LocationHidden bool               `json:"locationHidden"`
}

// Validate enforces contribution bounds independently at the client boundary.
func (footer PluginFooter) Validate() error {
	if len(footer.Items) > 64 {
		return errors.New("too many plugin footer items")
	}
	seen := make(map[string]bool)
	for _, item := range footer.Items {
		if !strings.HasPrefix(item.ID, item.PluginID+".") || !pluginCommandPluginID.MatchString(item.PluginID) || item.PluginID == "kit" || strings.HasPrefix(item.PluginID, "kit-") || !pluginCommandOwner.MatchString(item.Instance) || !pluginCommandLocalID.MatchString(strings.TrimPrefix(item.ID, item.PluginID+".")) || len(strings.TrimPrefix(item.ID, item.PluginID+".")) > 128 || seen[item.ID] || len(item.Content) == 0 || len(item.Content) > 32 {
			return errors.New("invalid plugin footer item")
		}
		seen[item.ID] = true
		total := 0
		for _, segment := range item.Content {
			total += len(segment.Text)
			if (segment.Text != "" && !validRendererText(segment.Text, 4096)) || total > 4096 {
				return errors.New("invalid plugin footer text")
			}
			for _, token := range []string{segment.Style.FG, segment.Style.BG} {
				if token != "" && !theme.IsKnownToken(token) {
					return errors.New("invalid plugin footer theme token")
				}
			}
		}
	}
	return nil
}
