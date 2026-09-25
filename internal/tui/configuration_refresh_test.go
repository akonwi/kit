package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type contextOverrideFunc func(string, int) error

func (f contextOverrideFunc) SetContextWindow(model string, capacity int) error {
	return f(model, capacity)
}

type configurationRefreshHarness struct{ state *configurationRefreshState }

func (w configurationRefreshHarness) CreateState() ui.State { return w.state }

type configurationRefreshState struct {
	inputOwnerTransitionState
	service     ModelOverrideService
	server      *fakeServer
	completions chan func()
}

// Use the real shell, input-owner routing and picker editing. Only substitute
// the save dependencies and queue async completion onto the test's UI thread.
func (s *configurationRefreshState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if key, ok := event.(ui.Key); ok && key.MatchString("Enter") && key.EventType != ui.EventRelease && key.EventType != vaxis.EventPaste && s.configurationPicker.EditingContext {
		s.saveModelContextWindowWith(s.service, s.server, func(f func()) { s.completions <- f })
		return ui.EventHandled
	}
	return s.appState.HandleEvent(ctx, event)
}

func mountConfigurationRefresh(t *testing.T) (*uitest.App, *configurationRefreshState) {
	t.Helper()
	state := &configurationRefreshState{inputOwnerTransitionState: *newInputOwnerTransitionState(), completions: make(chan func(), 4)}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	state.ctx = ctx
	generation := state.configurationPicker.Begin(configurationPickerModel, "test/current", "high")
	state.configurationPicker.Resolve(generation, refreshCatalog(128000), nil)
	state.configurationPicker.SetQuery("Test")
	state.configurationPicker.Select("test/other")
	application := uitest.New(configurationRefreshHarness{state})
	application.Pump(110, 30)
	return application, state
}

func refreshCatalog(capacity int) protocol.ModelCatalog {
	return protocol.ModelCatalog{Models: []protocol.ModelCapability{
		{ID: "test/current", Name: "Test Current", Available: true, ContextWindow: 32000},
		{ID: "test/other", Name: "Test Other", Available: true, ContextWindow: capacity},
	}}
}

func editConfigurationCapacity(t *testing.T, application *uitest.App, value string) {
	t.Helper()
	application.Send(vaxis.Key{Keycode: 'o', Modifiers: vaxis.ModCtrl})
	application.Pump(110, 30)
	assertConfigurationText(t, application, "enter save · blank clears · esc back")
	// The initial capacity is 128000 (six digits).
	for range 6 {
		application.Send(vaxis.Key{Keycode: vaxis.KeyBackspace})
	}
	for _, digit := range value {
		application.Key(string(digit))
	}
	application.Pump(110, 30)
	application.Enter()
	application.Pump(110, 30)
}

func assertConfigurationText(t *testing.T, application *uitest.App, expected string) {
	t.Helper()
	if !strings.Contains(application.Text(), expected) {
		t.Fatalf("missing %q:\n%s", expected, application.Text())
	}
}

func completeConfigurationRefresh(t *testing.T, application *uitest.App, state *configurationRefreshState) {
	t.Helper()
	select {
	case completion := <-state.completions:
		completion()
	case <-time.After(3 * time.Second):
		t.Fatal("catalog refresh did not complete")
	}
	application.Pump(110, 30)
}

func TestConfigurationCapacitySaveRefreshesMountedPicker(t *testing.T) {
	for _, tc := range []struct {
		name, value, expected string
		saved, capacity       int
	}{
		{"save", "256000", "256k context", 256000, 256000},
		{"clear", "", "64k context", 0, 64000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			application, state := mountConfigurationRefresh(t)
			saves := 0
			state.service = contextOverrideFunc(func(model string, capacity int) error {
				saves++
				if model != "test/other" || capacity != tc.saved {
					t.Fatalf("save = %q/%d", model, capacity)
				}
				return nil
			})
			state.server = &fakeServer{models: func() (protocol.ModelCatalog, error) { return refreshCatalog(tc.capacity), nil }}
			assertConfigurationText(t, application, "128k context")
			editConfigurationCapacity(t, application, tc.value)
			assertConfigurationText(t, application, "Loading…")
			completeConfigurationRefresh(t, application, state)
			assertConfigurationText(t, application, tc.expected)
			assertConfigurationText(t, application, "Test Other")
			assertConfigurationText(t, application, "↑↓ move · enter apply · ctrl+o overrides · esc close")
			if saves != 1 || state.configurationPicker.Selection != "test/other" || state.configurationPicker.Query != "Test" {
				t.Fatalf("picker after save = %+v, saves=%d", state.configurationPicker, saves)
			}
			// Reopening the editor must use the refreshed value, not its old catalog.
			application.Send(vaxis.Key{Keycode: 'o', Modifiers: vaxis.ModCtrl})
			application.Pump(110, 30)
			if tc.saved != 0 {
				assertConfigurationText(t, application, tc.value)
			} else {
				assertConfigurationText(t, application, "64000")
			}
		})
	}
}

func TestConfigurationCapacityRefreshFailureIsVisible(t *testing.T) {
	application, state := mountConfigurationRefresh(t)
	state.service = contextOverrideFunc(func(string, int) error { return nil })
	state.server = &fakeServer{models: func() (protocol.ModelCatalog, error) { return protocol.ModelCatalog{}, errors.New("offline") }}
	editConfigurationCapacity(t, application, "256000")
	completeConfigurationRefresh(t, application, state)
	assertConfigurationText(t, application, "Context-window override saved, but model refresh failed: offline")
	assertConfigurationText(t, application, "Test Other")
	if state.configurationPicker.Selection != "test/other" || state.configurationPicker.Query != "Test" {
		t.Fatalf("failed refresh lost selection/filter: %+v", state.configurationPicker)
	}
	// The picker remains usable so the user can retry the save/refresh.
	state.server.models = func() (protocol.ModelCatalog, error) { return refreshCatalog(256000), nil }
	editConfigurationCapacity(t, application, "256000")
	completeConfigurationRefresh(t, application, state)
	assertConfigurationText(t, application, "256k context")
}

func TestConfigurationCapacitySaveFailureKeepsEditor(t *testing.T) {
	application, state := mountConfigurationRefresh(t)
	state.service = contextOverrideFunc(func(string, int) error { return errors.New("settings are read-only") })
	state.server = &fakeServer{models: func() (protocol.ModelCatalog, error) { panic("must not refresh a failed save") }}
	editConfigurationCapacity(t, application, "256000")
	assertConfigurationText(t, application, "settings are read-only")
	assertConfigurationText(t, application, "256000")
	assertConfigurationText(t, application, "enter save · blank clears · esc back")
}

func TestConfigurationCapacityLateRefreshCannotChangeReopenedPicker(t *testing.T) {
	application, state := mountConfigurationRefresh(t)
	state.service = contextOverrideFunc(func(string, int) error { return nil })
	state.server = &fakeServer{models: func() (protocol.ModelCatalog, error) { return refreshCatalog(256000), nil }}
	editConfigurationCapacity(t, application, "256000")
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	application.Pump(110, 30)
	state.SetState(func() {
		generation := state.configurationPicker.Begin(configurationPickerModel, "test/current", "high")
		state.configurationPicker.Resolve(generation, refreshCatalog(512000), nil)
	})
	application.Pump(110, 30)
	completeConfigurationRefresh(t, application, state)
	assertConfigurationText(t, application, "512k context")
	if state.configurationPicker.Selection != "test/current" {
		t.Fatalf("reopened selection = %q", state.configurationPicker.Selection)
	}
}
