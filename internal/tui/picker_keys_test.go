package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

func pickerKeyTestCatalog() []pickerItem {
	return []pickerItem{
		{Key: "cd", Label: "cd", DisabledReason: "idle only"},
		{Key: "compact", Label: "compact", DisabledReason: "idle only"},
		{Key: "debug", Label: "debug"},
		{Key: "diff", Label: "diff"},
		{Key: "config", Label: "config"},
	}
}

func TestPickerKeyModelMovesWithWraparoundIncludingDisabledItems(t *testing.T) {
	t.Parallel()
	model := pickerKeyModel{Selection: "debug"}
	var visited []string
	for _, key := range []string{"Down", "Down", "Down", "Up", "Up"} {
		result := model.HandleKey(ui.Key{Keycode: map[string]rune{"Down": vaxis.KeyDown, "Up": vaxis.KeyUp}[key]}, pickerKeyTestCatalog())
		if !result.Handled || result.Activate || result.Dismiss || result.QueryChanged {
			t.Fatalf("%s result = %+v, want a handled move", key, result)
		}
		visited = append(visited, model.Selection)
	}
	want := []string{"diff", "config", "cd", "config", "diff"}
	for index := range want {
		if visited[index] != want[index] {
			t.Fatalf("selections = %v, want %v", visited, want)
		}
	}
}

func TestPickerKeyModelEditsQueryAndHighlightsFirstEnabledMatch(t *testing.T) {
	t.Parallel()
	var model pickerKeyModel
	model.SetQuery("", pickerKeyTestCatalog())
	if model.Selection != "debug" {
		t.Fatalf("initial selection = %q, want first enabled item", model.Selection)
	}
	// "c" matches the disabled cd and compact before config; the highlight
	// goes to the first enabled match.
	if result := model.HandleKey(ui.Key{Text: "c", Keycode: 'c'}, pickerKeyTestCatalog()); !result.QueryChanged || !result.Handled {
		t.Fatalf("typing c result = %+v", result)
	}
	if model.Query != "c" || model.Selection != "config" {
		t.Fatalf("after typing = query %q selection %q, want \"c\" and config", model.Query, model.Selection)
	}
	model.HandleKey(ui.Key{Text: "d", Keycode: 'd'}, pickerKeyTestCatalog())
	model.HandleKey(ui.Key{Keycode: vaxis.KeyBackspace}, pickerKeyTestCatalog())
	if model.Query != "c" || model.Selection != "config" {
		t.Fatalf("after backspace = query %q selection %q, want \"c\" and config", model.Query, model.Selection)
	}
	// Only disabled items match: the first one is highlighted so its reason is visible.
	model.SetQuery("compact", pickerKeyTestCatalog())
	if model.Selection != "compact" {
		t.Fatalf("disabled-only selection = %q, want compact", model.Selection)
	}
}

func TestPickerKeyModelActivatesDismissesAndFlattensPaste(t *testing.T) {
	t.Parallel()
	model := pickerKeyModel{Selection: "cd"}
	if result := model.HandleKey(ui.Key{Keycode: vaxis.KeyEnter}, pickerKeyTestCatalog()); !result.Activate || result.DisabledReason != "idle only" {
		t.Fatalf("enter on disabled item = %+v, want activation reporting its reason", result)
	}
	model.Selection = "diff"
	if result := model.HandleKey(ui.Key{Keycode: vaxis.KeyEnter}, pickerKeyTestCatalog()); !result.Activate || result.DisabledReason != "" {
		t.Fatalf("enter on enabled item = %+v", result)
	}
	if result := model.HandleKey(ui.Key{Keycode: vaxis.KeyEsc}, pickerKeyTestCatalog()); !result.Dismiss || !result.Handled {
		t.Fatalf("escape = %+v, want dismissal", result)
	}
	// Pasted Enter, Escape, and Tab become spaces in the query instead of acting.
	for _, key := range []ui.Key{
		{Text: "d", Keycode: 'd', EventType: vaxis.EventPaste},
		{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPaste},
		{Keycode: vaxis.KeyTab, EventType: vaxis.EventPaste},
	} {
		if result := model.HandleKey(key, pickerKeyTestCatalog()); result.Activate || result.Dismiss || !result.QueryChanged {
			t.Fatalf("paste %#v result = %+v, want query text only", key, result)
		}
	}
	if model.Query != "d  " {
		t.Fatalf("pasted query = %q, want \"d  \"", model.Query)
	}
}

func TestPickerKeyModelLeavesModifiedKeysAndReleasesUnhandled(t *testing.T) {
	t.Parallel()
	model := pickerKeyModel{Query: "d", Selection: "debug"}
	for _, key := range []ui.Key{
		{Text: "w", Keycode: 'w', Modifiers: vaxis.ModCtrl},
		{Text: "d", Keycode: 'd', EventType: ui.EventRelease},
	} {
		if result := model.HandleKey(key, pickerKeyTestCatalog()); result.Handled {
			t.Fatalf("key %#v was handled: %+v", key, result)
		}
	}
	// Other unmodified keys are consumed so they cannot reach the background.
	if result := model.HandleKey(ui.Key{Keycode: vaxis.KeyPgDown}, pickerKeyTestCatalog()); !result.Handled || result.QueryChanged || model.Selection != "debug" {
		t.Fatalf("page down = %+v selection %q, want consumed without paging", result, model.Selection)
	}
}

func TestPaletteAndModelPickerShareTheKeyModel(t *testing.T) {
	t.Parallel()
	// While work runs, "c" matches the idle-only cd and compact first; the
	// palette highlights the first enabled match instead.
	var palette paletteController
	palette.OpenFor(true)
	palette.HandleKey(true, ui.Key{Text: "c", Keycode: 'c'})
	if want := paletteCommandID(firstEnabledPickerKey(filterPaletteItems("c", paletteCatalog(true, nil)))); palette.Selection != want || want == paletteCommandCD {
		t.Fatalf("running palette selection = %q, want first enabled match %q", palette.Selection, want)
	}
	if _, run, handled := palette.HandleKey(true, ui.Key{Keycode: vaxis.KeyEsc}); run || !handled || palette.Open {
		t.Fatalf("escape left palette open=%v run=%v handled=%v", palette.Open, run, handled)
	}

	// The model picker wraps, filters, and dismisses through the same model.
	picker := configurationPickerController{}
	generation := picker.Begin(configurationPickerModel, "b/second", "")
	picker.Resolve(generation, protocol.ModelCatalog{Models: []protocol.ModelCapability{
		{ID: "a/first", Name: "First", Available: true}, {ID: "b/second", Name: "Second", Available: true},
	}}, nil)
	picker.HandleKey(ui.Key{Keycode: vaxis.KeyDown})
	if picker.Selection != "a/first" {
		t.Fatalf("model picker down from the last model = %q, want wraparound to a/first", picker.Selection)
	}
	picker.HandleKey(ui.Key{Text: "s", Keycode: 's'})
	if picker.Query != "s" || picker.Selection != "b/second" {
		t.Fatalf("model picker typing = query %q selection %q", picker.Query, picker.Selection)
	}
	if apply, handled := picker.HandleKey(ui.Key{Keycode: vaxis.KeyEnter}); !apply || !handled {
		t.Fatalf("model picker enter = apply:%v handled:%v", apply, handled)
	}
	picker.HandleKey(ui.Key{Keycode: vaxis.KeyEsc})
	if picker.Mode != configurationPickerClosed {
		t.Fatalf("model picker escape left mode %v", picker.Mode)
	}
}

func pickerItemKeys(items []pickerItem) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return keys
}

func TestPickerKeyModelNavigationModeLeavesEditingKeysToTheComposer(t *testing.T) {
	t.Parallel()
	model := pickerKeyModel{Query: "d", Selection: "diff"}
	// "d" matches debug, diff, then the disabled cd; navigation wraps at
	// both ends.
	var visited []string
	for _, key := range []ui.Key{{Keycode: vaxis.KeyDown}, {Keycode: vaxis.KeyDown}, {Keycode: vaxis.KeyUp}} {
		result := model.HandleNavigationKey(key, pickerKeyTestCatalog())
		if !result.Handled || result.QueryChanged || result.Activate || result.Dismiss {
			t.Fatalf("move result = %+v, want a handled move", result)
		}
		visited = append(visited, model.Selection)
	}
	if got, want := visited, []string{"cd", "debug", "cd"}; got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("selections = %v, want %v", got, want)
	}
	if result := model.HandleNavigationKey(ui.Key{Keycode: vaxis.KeyUp}, pickerKeyTestCatalog()); result.Moved != -1 {
		t.Fatalf("Up moved = %d, want -1", result.Moved)
	}
	if result := model.HandleNavigationKey(ui.Key{Keycode: vaxis.KeyEnter}, pickerKeyTestCatalog()); !result.Handled || !result.Activate {
		t.Fatalf("Enter result = %+v, want activation", result)
	}
	if result := model.HandleNavigationKey(ui.Key{Keycode: vaxis.KeyEsc}, pickerKeyTestCatalog()); !result.Handled || !result.Dismiss {
		t.Fatalf("Escape result = %+v, want dismissal", result)
	}
	// Text, deletion, cursor movement, and paste belong to the composer.
	for name, key := range map[string]ui.Key{
		"text":      {Text: "x", Keycode: 'x'},
		"backspace": {Keycode: vaxis.KeyBackspace},
		"ctrl+bs":   {Keycode: vaxis.KeyBackspace, Modifiers: vaxis.ModCtrl},
		"left":      {Keycode: vaxis.KeyLeft},
		"right":     {Keycode: vaxis.KeyRight},
		"tab":       {Keycode: vaxis.KeyTab},
		"paste":     {Text: "pasted", EventType: vaxis.EventPaste},
		"release":   {Keycode: vaxis.KeyDown, EventType: ui.EventRelease},
	} {
		if result := model.HandleNavigationKey(key, pickerKeyTestCatalog()); result.Handled {
			t.Fatalf("%s result = %+v, want unhandled", name, result)
		}
	}
	if model.Query != "d" {
		t.Fatalf("query = %q, want the navigation mode to leave it unchanged", model.Query)
	}
}
