package tui

// pickerItem is one picker row. Every row is a single line in uniform columns:
// label, optional hint, optional description, and optional trailing metadata.
// Current marks the active value; DisabledReason makes the row unavailable.
//
// Hierarchical pickers set Depth to indent the label two cells per level, and
// Disclosure with ChildCount to show "▸ N" or "▾ N" at the start of the hint
// column, followed by Hint when both are present.
type pickerItem struct {
	Key            string
	Label          string
	Hint           string
	Description    string
	Meta           string
	Current        bool
	DisabledReason string
	Depth          int
	Disclosure     pickerDisclosure
	ChildCount     int
}

// pickerDisclosure is the expand state of an item that has children.
type pickerDisclosure uint8

const (
	pickerDisclosureNone pickerDisclosure = iota
	pickerDisclosureCollapsed
	pickerDisclosureExpanded
)

// pickerTone colors picker messages and footer status.
type pickerTone uint8

const (
	pickerToneMuted pickerTone = iota
	pickerToneDanger
	pickerToneLoading
)
