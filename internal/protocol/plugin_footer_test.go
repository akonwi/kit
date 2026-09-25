package protocol

import (
	"strings"
	"testing"
)

func TestPluginFooterValidation(t *testing.T) {
	valid := PluginFooter{LocationHidden: true, Items: []PluginFooterItem{{ID: "demo.status", PluginID: "demo", Instance: "host:1", Content: []PluginFooterSegment{{Text: "Ready", Style: PluginFooterStyle{FG: "toolText", Bold: true}}}}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	empty := valid.Items[0]
	empty.Content = []PluginFooterSegment{{Text: "", Style: PluginFooterStyle{Bold: true}}}
	if err := (PluginFooter{Items: []PluginFooterItem{empty}}).Validate(); err != nil {
		t.Fatalf("schema-valid empty segment: %v", err)
	}

	for _, mutate := range []func(*PluginFooterItem){func(i *PluginFooterItem) { i.PluginID = "other" }, func(i *PluginFooterItem) { i.Instance = "" }, func(i *PluginFooterItem) { i.Content = []PluginFooterSegment{{Text: "\x1b"}} }, func(i *PluginFooterItem) {
		i.Content = []PluginFooterSegment{{Text: "ok", Style: PluginFooterStyle{FG: "#abcdef"}}}
	}, func(i *PluginFooterItem) { i.Content = []PluginFooterSegment{{Text: strings.Repeat("x", 4097)}} }} {
		item := valid.Items[0]
		mutate(&item)
		if err := (PluginFooter{Items: []PluginFooterItem{item}}).Validate(); err == nil {
			t.Fatalf("accepted invalid footer %#v", item)
		}
	}
	if err := (PluginFooter{Items: []PluginFooterItem{valid.Items[0], valid.Items[0]}}).Validate(); err == nil {
		t.Fatal("duplicate footer ids accepted")
	}
}
