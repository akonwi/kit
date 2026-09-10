package tui

import (
	"errors"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

func TestCompactionAndConfigurationPendingCountAsActiveWork(t *testing.T) {
	if !(&appState{compactPending: true}).hasActiveWork() {
		t.Fatal("compaction was not treated as active work")
	}
	if !(&appState{configurationPicker: configurationPickerController{Pending: true}}).hasActiveWork() {
		t.Fatal("configuration application was not treated as active work")
	}
}

func TestConfigurationPickerFiltersMovesAndPreservesFailedSelection(t *testing.T) {
	catalog := protocol.ModelCatalog{Models: []protocol.ModelCapability{
		{ID: "anthropic/claude", Name: "Claude", Provider: "anthropic", Available: true},
		{ID: "openai/gpt", Name: "GPT", Provider: "openai", Available: true},
		{ID: "other/unavailable", Name: "Unavailable", Provider: "other", Available: false},
	}}
	var controller configurationPickerController
	generation := controller.Begin(configurationPickerModel, "openai/gpt", "high")
	if !controller.Resolve(generation, catalog, nil) || controller.Selection != "openai/gpt" {
		t.Fatalf("resolved model picker = %+v", controller)
	}
	if models := controller.filteredModels(); len(models) != 2 || models[0].ID != "anthropic/claude" || models[1].ID != "openai/gpt" {
		t.Fatalf("authenticated model options = %+v", models)
	}
	controller.SetQuery("claude")
	if controller.Selection != "anthropic/claude" || len(controller.filteredModels()) != 1 {
		t.Fatalf("filtered model picker = %+v", controller)
	}
	applyGeneration, selection, ok := controller.BeginApply()
	if !ok || selection != "anthropic/claude" || !controller.Pending {
		t.Fatalf("begin apply = generation:%d selection:%q ok:%v state:%+v", applyGeneration, selection, ok, controller)
	}
	if !controller.ResolveApply(applyGeneration, errors.New("session is busy")) || controller.Mode != configurationPickerModel ||
		controller.Selection != "anthropic/claude" || controller.Query != "claude" || controller.Pending {
		t.Fatalf("failed apply did not preserve selector state: %+v", controller)
	}
	if apply, handled := controller.HandleKey(ui.Key{Keycode: 'x'}); apply || handled {
		t.Fatalf("ordinary model key was unexpectedly handled before editor fallback")
	}
}

func TestConfigurationSelectionBuildsAtomicModelAndThinkingRequests(t *testing.T) {
	session := protocol.SessionInfo{Model: "test/large", ThinkingLevel: "medium", ConfigurationRevision: 7}
	model := configurationInputForSelection(session, configurationPickerModel, "test/small")
	if model.ExpectedRevision != 7 || model.Model != "test/small" || model.ThinkingLevel != nil {
		t.Fatalf("model selection request = %+v", model)
	}
	thinking := configurationInputForSelection(session, configurationPickerThinking, "high")
	if thinking.ExpectedRevision != 7 || thinking.Model != "test/large" || thinking.ThinkingLevel == nil || *thinking.ThinkingLevel != protocol.ThinkingHigh {
		t.Fatalf("thinking selection request = %+v", thinking)
	}
	if toast := compactionToast(protocol.CompactSessionResult{}, nil, nil); toast.Title != "Compaction failed" || toast.Subtitle != "Not enough turns to compact." || toast.Variant != toastError {
		t.Fatalf("no-op compaction toast = %+v", toast)
	}
	if toast := compactionToast(protocol.CompactSessionResult{Compacted: true}, nil, nil); toast.Title != "Session compacted" || toast.Subtitle != "Session context was compacted." || toast.Variant != toastInfo {
		t.Fatalf("changed compaction toast = %+v", toast)
	}
	failure := errors.New("provider unavailable")
	if toast := compactionToast(protocol.CompactSessionResult{}, failure, nil); toast.Title != "Compaction failed" || toast.Subtitle != failure.Error() || toast.Variant != toastError {
		t.Fatalf("failed compaction toast = %+v", toast)
	}
}

func TestThinkingPickerOffersOnlyActiveModelLevels(t *testing.T) {
	catalog := protocol.ModelCatalog{Models: []protocol.ModelCapability{
		{ID: "test/current", Available: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingHigh}},
		{ID: "test/other", Available: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingMedium}},
	}}
	var controller configurationPickerController
	generation := controller.Begin(configurationPickerThinking, "test/current", "high")
	controller.Resolve(generation, catalog, nil)
	levels := controller.thinkingLevels()
	if len(levels) != 2 || levels[0] != "off" || levels[1] != "high" || controller.Selection != "high" {
		t.Fatalf("thinking levels = %#v, controller=%+v", levels, controller)
	}
	controller.Move(1)
	if controller.Selection != "off" {
		t.Fatalf("wrapped thinking selection = %q, want off", controller.Selection)
	}
}
