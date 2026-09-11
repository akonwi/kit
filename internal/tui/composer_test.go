package tui

import (
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestComposerWordDeletionShortcuts(t *testing.T) {
	t.Parallel()

	ctrlW := vaxis.Key{Keycode: 'w', Modifiers: vaxis.ModCtrl}
	ctrlShiftW := vaxis.Key{Keycode: 'w', Modifiers: vaxis.ModCtrl | vaxis.ModShift}
	home := vaxis.Key{Keycode: vaxis.KeyHome}

	cases := []struct {
		name string
		text string
		keys []vaxis.Key
		want string
	}{
		{name: "ctrl+w deletes previous word", text: "hello brave world", keys: []vaxis.Key{ctrlW}, want: "hello brave "},
		{name: "ctrl+w repeats across words", text: "hello world", keys: []vaxis.Key{ctrlW, ctrlW}, want: ""},
		{name: "ctrl+shift+w deletes next word", text: "hello world", keys: []vaxis.Key{home, ctrlShiftW}, want: " world"},
		{name: "ctrl+shift+w repeats across words", text: "hello world", keys: []vaxis.Key{home, ctrlShiftW, ctrlShiftW}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const width, height = 40, 12
			state := &shellHarnessState{}
			app := uitest.New(shellHarness{State: state})
			app.Pump(width, height)
			for _, r := range tc.text {
				if r == '\n' {
					app.Send(vaxis.Key{Keycode: vaxis.KeyEnter, Modifiers: vaxis.ModShift})
				} else {
					app.Key(string(r))
				}
			}
			app.Pump(width, height)
			if state.composer != tc.text {
				t.Fatalf("typed composer = %q, want %q", state.composer, tc.text)
			}
			for _, key := range tc.keys {
				app.Send(key)
				app.Pump(width, height)
			}
			if state.composer != tc.want {
				t.Fatalf("composer after %s = %q, want %q", tc.name, state.composer, tc.want)
			}
		})
	}
}
