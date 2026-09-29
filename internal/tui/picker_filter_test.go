package tui

import (
	"reflect"
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

func TestFilterPickerItemsMatchesLabelSearchTextAndAliases(t *testing.T) {
	t.Parallel()
	catalog := []pickerItem{
		{Key: "claude", Label: "Claude", Description: "shown, not matched", SearchText: "anthropic/claude", SearchAliases: []string{"anthropic"}},
		{Key: "gpt", Label: "GPT", SearchText: "openai/gpt", SearchAliases: []string{"openai"}},
		{Key: "auto", Label: "Auto router", SearchText: "router/auto"},
	}
	for _, test := range []struct {
		query string
		want  []string
	}{
		{query: "", want: []string{"claude", "gpt", "auto"}},
		{query: "  ", want: []string{"claude", "gpt", "auto"}},
		{query: "GPT", want: []string{"gpt"}},
		{query: "openai", want: []string{"gpt"}},
		{query: "anthropic/cl", want: []string{"claude"}},
		// Labels outrank search text: "auto" is Auto router's label prefix.
		{query: "auto", want: []string{"auto"}},
		{query: "shown", want: []string{}},
	} {
		if got := pickerItemKeys(filterPickerItems(test.query, catalog)); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("filter %q = %v, want %v", test.query, got, test.want)
		}
	}
}

func TestPickerKeyModelUsesItsFilterHook(t *testing.T) {
	t.Parallel()
	catalog := paletteCatalog(false, nil)
	// The palette hook matches only the command word, so arguments keep the
	// command highlighted.
	model := pickerKeyModel{Filter: filterPaletteItems}
	for _, character := range "theme dark" {
		model.HandleKey(ui.Key{Text: string(character), Keycode: character}, catalog)
	}
	if model.Query != "theme dark" || model.Selection != "theme" {
		t.Fatalf("palette hook = query %q selection %q, want \"theme dark\" and theme", model.Query, model.Selection)
	}
	// refresh-models matches "theme" fuzzily in its description.
	if got := pickerItemKeys(model.Items(catalog)); !reflect.DeepEqual(got, []string{"theme", "refresh-models"}) {
		t.Fatalf("palette rows = %v, want [theme refresh-models]", got)
	}
	// The default filter matches the whole query, which no command does.
	model.Filter = nil
	model.SetQuery(model.Query, catalog)
	if model.Selection != "" || len(model.Items(catalog)) != 0 {
		t.Fatalf("default filter = selection %q rows %v, want no match", model.Selection, pickerItemKeys(model.Items(catalog)))
	}
}

func TestPickerKeyModelDeletesWordsLikeTheTextField(t *testing.T) {
	t.Parallel()
	model := pickerKeyModel{Query: "review auth-module "}
	for _, want := range []string{"review auth-", "review auth", "review ", ""} {
		if result := model.HandleKey(ui.Key{Keycode: vaxis.KeyBackspace, Modifiers: vaxis.ModCtrl}, pickerKeyTestCatalog()); !result.QueryChanged {
			t.Fatalf("ctrl+backspace from %q = %+v", model.Query, result)
		}
		if model.Query != want {
			t.Fatalf("ctrl+backspace query = %q, want %q", model.Query, want)
		}
	}
	model.Query = "rel "
	model.HandleKey(ui.Key{Keycode: vaxis.KeyBackspace, Modifiers: vaxis.ModAlt}, pickerKeyTestCatalog())
	if model.Query != "rel" {
		t.Fatalf("alt+backspace query = %q, want \"rel\"", model.Query)
	}
}

func TestWorkspaceFileFilterMatchesDirectoriesByBareName(t *testing.T) {
	t.Parallel()
	catalog := []pickerItem{{Key: "file", Label: "internal.go"}, {Key: "dir", Label: "internal/", Meta: "directory"}}
	// The directory is an exact match for its bare name; the default filter
	// sees "internal/" as a prefix match tied with internal.go.
	if got := pickerItemKeys(filterWorkspaceFileItems("internal", catalog)); !reflect.DeepEqual(got, []string{"dir", "file"}) {
		t.Fatalf("file hook = %v, want [dir file]", got)
	}
	if got := pickerItemKeys(filterPickerItems("internal", catalog)); !reflect.DeepEqual(got, []string{"file", "dir"}) {
		t.Fatalf("default filter = %v, want [file dir]", got)
	}
}

func pickerTreeTestCatalog() []pickerItem {
	return []pickerItem{
		{Key: "auth", Label: "Authentication", Disclosure: pickerDisclosureExpanded, ChildCount: 2},
		{Key: "oauth", Label: "OAuth", Depth: 1, ParentKey: "auth", Disclosure: pickerDisclosureCollapsed, ChildCount: 1},
		{Key: "tokens", Label: "Tokens", Depth: 2, ParentKey: "oauth"},
		{Key: "keys", Label: "Keys", Depth: 1, ParentKey: "auth", DisabledReason: "archived"},
		{Key: "release", Label: "Release", SearchText: "~/kit/tokens-release"},
	}
}

func TestFilterPickerTreeKeepsMatchesUnderTheirAncestors(t *testing.T) {
	t.Parallel()
	catalog := pickerTreeTestCatalog()
	// A blank query shows the tree with collapsed rows hiding their children.
	if got := pickerItemKeys(filterPickerTree(" ", catalog)); !reflect.DeepEqual(got, []string{"auth", "oauth", "keys", "release"}) {
		t.Fatalf("blank tree = %v", got)
	}
	// "tokens" ranks Tokens (label) above Release (search text); Tokens's
	// family is placed first and shows its collapsed ancestors, open and
	// without disclosures.
	rows := filterPickerTree("tokens", catalog)
	if got := pickerItemKeys(rows); !reflect.DeepEqual(got, []string{"auth", "oauth", "tokens", "release"}) {
		t.Fatalf("filtered tree = %v", got)
	}
	for _, row := range rows {
		if row.Disclosure != pickerDisclosureNone || row.ChildCount != 0 {
			t.Fatalf("filtered row %q disclosure = %v %d, want none", row.Key, row.Disclosure, row.ChildCount)
		}
	}
	// A family moves with its best match: Release ranks first for "rel".
	if got := pickerItemKeys(filterPickerTree("rel", catalog)); !reflect.DeepEqual(got, []string{"release"}) {
		t.Fatalf("release tree = %v", got)
	}
}

func TestPickerKeyModelHighlightsFirstEnabledRowOfItsTreeFilter(t *testing.T) {
	t.Parallel()
	// The first visible row is an ancestor; the first enabled match of
	// "keys" is shown under it, and disabled rows are still highlighted when
	// they are all that matches.
	model := pickerKeyModel{Filter: filterPickerTree}
	model.SetQuery("o", pickerTreeTestCatalog())
	if got := pickerItemKeys(model.Items(pickerTreeTestCatalog())); model.Selection != "auth" || !reflect.DeepEqual(got, []string{"auth", "oauth", "tokens", "release"}) {
		t.Fatalf("tree model = selection %q rows %v", model.Selection, got)
	}
	model.SetQuery("keys", pickerTreeTestCatalog())
	if model.Selection != "auth" {
		t.Fatalf("tree model with a disabled match = %q, want the enabled ancestor", model.Selection)
	}
	catalog := []pickerItem{{Key: "a", Label: "alpha", DisabledReason: "idle only"}, {Key: "b", Label: "beta", DisabledReason: "idle only"}}
	model = pickerKeyModel{}
	model.SetQuery("a", catalog)
	if model.Selection != "a" {
		t.Fatalf("disabled-only selection = %q, want a", model.Selection)
	}
}
