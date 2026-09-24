package session

// PluginFooterStyle retains renderer-neutral semantic token styles.
type PluginFooterStyle struct {
	FG            string
	BG            string
	Bold          bool
	Dim           bool
	Italic        bool
	Underline     bool
	Strikethrough bool
}

// PluginFooterSegment is one styled run of footer text.
type PluginFooterSegment struct {
	Text  string
	Style PluginFooterStyle
}

// PluginFooterItem is a generation-owned bottom-right contribution.
type PluginFooterItem struct {
	ID       string
	PluginID string
	Instance string
	Content  []PluginFooterSegment
}

// PluginFooter projects visible ordered items and aggregate location hiding.
type PluginFooter struct {
	Items          []PluginFooterItem
	LocationHidden bool
}

// PluginFooterHost supplies a nonblocking generation-filtered snapshot.
type PluginFooterHost interface{ Footer() PluginFooter }
