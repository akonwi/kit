package tui

import (
	"errors"
	"reflect"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

func TestCompactionAndConfigurationPendingCountAsActiveWork(t *testing.T) {
	if !(&appState{compactPending: true}).hasActiveWork() {
		t.Fatal("compaction was not treated as active work")
	}
	if !(&appState{configurationPicker: configurationPickerController{Pending: true}}).hasActiveWork() {
		t.Fatal("configuration application was not treated as active work")
	}
	if (&appState{configurationPicker: configurationPickerController{
		Pending: true, Target: configurationTarget{ConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}}).hasActiveWork() {
		t.Fatal("subagent configuration blocked the main session")
	}
}

func TestConfigurationPickerEditsSelectedModelContextWindow(t *testing.T) {
	controller := configurationPickerController{}
	generation := controller.Begin(configurationPickerModel, "openai-codex/gpt-5.6-sol", "medium")
	controller.Resolve(generation, protocol.ModelCatalog{Models: []protocol.ModelCapability{{
		ID: "openai-codex/gpt-5.6-sol", Name: "GPT-5.6 Sol", Available: true, ContextWindow: 1_000_000,
	}}}, nil)
	if !controller.BeginContextEdit() || controller.EditModel != "openai-codex/gpt-5.6-sol" || controller.EditValue != "1000000" {
		t.Fatalf("context editor = %+v", controller)
	}
	controller.HandleKey(ui.Key{Keycode: vaxis.KeyBackspace, EventType: vaxis.EventPress})
	if controller.EditValue != "100000" {
		t.Fatalf("edited value = %q", controller.EditValue)
	}
	controller.HandleKey(ui.Key{Keycode: vaxis.KeyEsc, EventType: vaxis.EventPress})
	if controller.EditingContext || controller.Mode != configurationPickerModel {
		t.Fatalf("escape did not return to model picker: %+v", controller)
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
	if got := pickerItemKeys(controller.keyModel().Items(controller.pickerCatalog())); !reflect.DeepEqual(got, []string{"anthropic/claude", "openai/gpt"}) {
		t.Fatalf("authenticated model options = %v", got)
	}
	for _, character := range "claude" {
		controller.HandleKey(ui.Key{Text: string(character), Keycode: character})
	}
	if got := pickerItemKeys(controller.keyModel().Items(controller.pickerCatalog())); controller.Selection != "anthropic/claude" || !reflect.DeepEqual(got, []string{"anthropic/claude"}) {
		t.Fatalf("filtered model picker = %+v rows %v", controller, got)
	}
	applyGeneration, selection, ok := controller.BeginApply()
	if !ok || selection != "anthropic/claude" || !controller.Pending {
		t.Fatalf("begin apply = generation:%d selection:%q ok:%v state:%+v", applyGeneration, selection, ok, controller)
	}
	if !controller.ResolveApply(applyGeneration, errors.New("session is busy")) || controller.Mode != configurationPickerModel ||
		controller.Selection != "anthropic/claude" || controller.Query != "claude" || controller.Pending {
		t.Fatalf("failed apply did not preserve selector state: %+v", controller)
	}
	// Text goes straight into the query through the shared picker key model.
	if apply, handled := controller.HandleKey(ui.Key{Text: "x", Keycode: 'x'}); apply || !handled || controller.Query != "claudex" {
		t.Fatalf("typed model key = apply:%v handled:%v query:%q", apply, handled, controller.Query)
	}
}

func TestSubagentConfigurationSelectionBuildsPatchRequests(t *testing.T) {
	target := configurationTarget{
		ConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AgentName: "reviewer", Generation: 4,
	}
	model := subagentConfigurationInput(target, configurationPickerModel, "test/small")
	if model.Generation != 4 || model.Model == nil || *model.Model != "test/small" || model.ThinkingLevel != nil {
		t.Fatalf("subagent model patch = %+v", model)
	}
	thinking := subagentConfigurationInput(target, configurationPickerThinking, "high")
	if thinking.Generation != 4 || thinking.Model != nil || thinking.ThinkingLevel == nil || *thinking.ThinkingLevel != protocol.ThinkingHigh {
		t.Fatalf("subagent thinking patch = %+v", thinking)
	}
	controller := configurationPickerController{}
	controller.BeginTarget(configurationPickerModel, target, "test/current", "medium")
	controller.Loading = false
	controller.Selection = "test/current"
	controller.Models = []protocol.ModelCapability{{ID: "test/current", Available: true}}
	if controller.BeginContextEdit() {
		t.Fatal("subagent model picker exposed global context-window overrides")
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
