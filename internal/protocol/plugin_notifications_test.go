package protocol

import (
	"strings"
	"testing"
)

func TestPluginToastValidation(t *testing.T) {
	toast := PluginToast{PluginID: "demo", Instance: "owner:1", Title: "Notice", Subtitle: "first\nsecond\tline", Variant: "warning", Persistent: true}
	if err := toast.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PluginToast){func(v *PluginToast) { v.Title = " " }, func(v *PluginToast) { v.Variant = "success" }, func(v *PluginToast) { v.Subtitle = "escape\x1b" }, func(v *PluginToast) { v.Subtitle = strings.Repeat("x", 4097) }, func(v *PluginToast) { v.PluginID = "Bad" }, func(v *PluginToast) { v.Instance = "" }} {
		invalid := toast
		change(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatalf("accepted invalid toast %#v", invalid)
		}
	}
}
