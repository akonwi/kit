package protocol

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PluginToast is a live-only session notification, never a replayable event.
type PluginToast struct {
	PluginID   string `json:"pluginId"`
	Instance   string `json:"instance"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle,omitempty"`
	Variant    string `json:"variant"`
	Persistent bool   `json:"persistent,omitempty"`
}

// Validate checks bounded notification metadata before transport or rendering.
func (t PluginToast) Validate() error {
	if !pluginCommandPluginID.MatchString(t.PluginID) || !pluginCommandOwner.MatchString(t.Instance) || !validRendererText(t.Title, 1024) || strings.TrimSpace(t.Title) == "" || len(t.Subtitle) > 4096 || !utf8.ValidString(t.Subtitle) || (t.Variant != "info" && t.Variant != "warning" && t.Variant != "error") {
		return errors.New("invalid plugin toast")
	}
	for _, r := range t.Subtitle {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) {
			return errors.New("invalid plugin toast subtitle")
		}
	}
	return nil
}
