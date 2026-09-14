package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/systemprompt"
)

func TestRegistryContainsEmbeddedCustomizationSkill(t *testing.T) {
	registry := mustRegistry(t)
	skill, ok := registry.Lookup(KitCustomizationName)
	if !ok {
		t.Fatal("registry does not contain kit-customization")
	}
	if skill.Source != SourceBuiltin || skill.Location != "" {
		t.Fatalf("embedded skill metadata = %#v", skill)
	}
	for _, expected := range []string{
		"https://github.com/akonwi/kit",
		"Inspect the relevant documentation and source",
		"Prefer documented user-editable customization surfaces",
		"Put global context guidance in `AGENTS.md` under the resolved Kit home.",
	} {
		if !strings.Contains(skill.Content, expected) {
			t.Fatalf("embedded skill does not contain %q:\n%s", expected, skill.Content)
		}
	}
}

func TestRegistryRejectsReservedDuplicateAndCallerBuiltins(t *testing.T) {
	collision := Skill{
		Name: KitCustomizationName, Description: "shadow", Content: "shadow", Source: SourceProject,
	}
	if _, err := NewRegistry(collision); !errors.Is(err, ErrReservedName) {
		t.Fatalf("reserved collision error = %v, want ErrReservedName", err)
	}

	skill := Skill{Name: "review", Description: "Review changes", Content: "Review carefully.", Source: SourceUser}
	if _, err := NewRegistry(skill, skill); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate error = %v, want ErrDuplicateName", err)
	}
	callerBuiltin := Skill{Name: "other", Description: "Other", Content: "Other", Source: SourceBuiltin}
	if _, err := NewRegistry(callerBuiltin); err == nil {
		t.Fatal("NewRegistry() accepted a caller-supplied built-in")
	}
}

func TestRegistryValidatesAndBoundsDefinitions(t *testing.T) {
	valid := Skill{Name: "review", Description: "Review", Content: "Instructions", Source: SourceUser}
	tests := []Skill{
		{Name: "", Description: "Review", Content: "Instructions", Source: SourceUser},
		{Name: "Bad-Name", Description: "Review", Content: "Instructions", Source: SourceUser},
		{Name: "bad--name", Description: "Review", Content: "Instructions", Source: SourceUser},
		{Name: "review", Description: "", Content: "Instructions", Source: SourceUser},
		{Name: "review", Description: "Review", Content: "", Source: SourceUser},
		{Name: "review", Description: "Review\x01", Content: "Instructions", Source: SourceUser},
		{Name: "review", Description: "Review", Content: strings.Repeat("x", maxSkillContentBytes+1), Source: SourceUser},
		{Name: "review", Description: "Review", Content: "Instructions", Source: SourceUser, Location: strings.Repeat("x", maxSkillLocationBytes+1)},
		{Name: "review", Description: "Review", Content: "Instructions", Source: "invalid"},
	}
	for _, skill := range tests {
		if _, err := NewRegistry(skill); err == nil {
			t.Fatalf("NewRegistry(%#v) succeeded", skill)
		}
	}
	if _, err := NewRegistry(valid); err != nil {
		t.Fatalf("NewRegistry(valid) error = %v", err)
	}

	tooMany := make([]Skill, maxSkillsPerRegistry)
	for index := range tooMany {
		tooMany[index] = Skill{
			Name: fmt.Sprintf("skill-%d", index), Description: "Skill", Content: "Skill", Source: SourceProject,
		}
	}
	if _, err := NewRegistry(tooMany...); err == nil {
		t.Fatal("NewRegistry() accepted too many skills")
	}
}

func TestRegistryOrdersSkillsByStableName(t *testing.T) {
	registry, err := NewRegistry(
		Skill{Name: "z-last", Description: "Last", Content: "Last", Source: SourcePlugin},
		Skill{Name: "a-first", Description: "First", Content: "First", Source: SourceProject},
	)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0)
	for _, skill := range registry.Skills() {
		names = append(names, skill.Name)
	}
	want := []string{"a-first", KitCustomizationName, "z-last"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("skill names = %#v, want %#v", names, want)
	}
}

func TestCatalogSectionFormatsDeterministicEscapedMetadata(t *testing.T) {
	registry, err := NewRegistry(
		Skill{
			Name: "z-last", Description: `Use for <z> & "quotes"`, Content: "z", Source: SourcePlugin,
		},
		Skill{
			Name: "a-first", Description: "Use for a", Content: "a", Source: SourceProject,
			Location: `/repo/a&b/SKILL.md`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	section, err := registry.CatalogSection()
	if err != nil {
		t.Fatal(err)
	}
	const want = `The following skills provide specialized instructions for specific tasks.
Call the activate_skill tool with the skill name to activate it when the task matches its description.
When a skill's instructions reference a relative path, resolve it against the skill directory and use that absolute path in tool commands.

<available_skills>
  <skill>
    <name>a-first</name>
    <description>Use for a</description>
    <source>project</source>
    <location>/repo/a&amp;b/SKILL.md</location>
  </skill>
  <skill>
    <name>kit-customization</name>
    <description>Use when the user asks about Kit itself, its behavior, documentation, or supported customization surfaces.</description>
    <source>built-in</source>
  </skill>
  <skill>
    <name>z-last</name>
    <description>Use for &lt;z&gt; &amp; &quot;quotes&quot;</description>
    <source>plugin</source>
  </skill>
</available_skills>`
	if section.ID != catalogSectionID || section.Kind != systemprompt.SectionSkillCatalog || section.Text != want {
		t.Fatalf("CatalogSection() = %#v\nwant text:\n%s", section, want)
	}
	wantSources := []systemprompt.Source{
		{ID: "skill:a-first", Path: `/repo/a&b/SKILL.md`},
		{ID: "skill:" + KitCustomizationName},
		{ID: "skill:z-last"},
	}
	if !reflect.DeepEqual(section.Sources, wantSources) {
		t.Fatalf("catalog sources = %#v, want %#v", section.Sources, wantSources)
	}
}

func TestCatalogSectionEnforcesAggregateBound(t *testing.T) {
	additions := make([]Skill, maxSkillsPerRegistry-1)
	for index := range additions {
		additions[index] = Skill{
			Name:        fmt.Sprintf("skill-%d", index),
			Description: strings.Repeat("d", maxDescriptionBytes),
			Content:     "content",
			Source:      SourceProject,
			Location:    strings.Repeat("l", maxSkillLocationBytes),
		}
	}
	registry, err := NewRegistry(additions...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.CatalogSection(); err == nil {
		t.Fatal("CatalogSection() accepted an oversized catalog")
	}
}

func TestActivateSkillReturnsEmbeddedInstructionsAndDetails(t *testing.T) {
	registry := mustRegistry(t)
	tool := registry.newActivateTool()
	result, err := tool.Execute(context.Background(), droids.ToolContext{}, activateArgs{Name: KitCustomizationName}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("activation result = %#v", result)
	}
	skill, _ := registry.Lookup(KitCustomizationName)
	if got := toolResultText(t, result); got != skill.Content {
		t.Fatalf("activation text = %q, want %q", got, skill.Content)
	}
	var details activateDetails
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	want := activateDetails{Name: KitCustomizationName, Found: true, Source: SourceBuiltin}
	if details != want {
		t.Fatalf("activation details = %#v, want %#v", details, want)
	}
}

func TestActivateSkillReportsUnknownName(t *testing.T) {
	registry := mustRegistry(t)
	tool := registry.newActivateTool()
	result, err := tool.Execute(context.Background(), droids.ToolContext{}, activateArgs{Name: "missing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("unknown activation result = %#v", result)
	}
	want := `Unknown skill "missing". Available skills: kit-customization.`
	if got := toolResultText(t, result); got != want {
		t.Fatalf("unknown activation text = %q, want %q", got, want)
	}
	var details activateDetails
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details != (activateDetails{Name: "missing"}) {
		t.Fatalf("unknown activation details = %#v", details)
	}
}

func TestActivateSkillToolHasStrictSchema(t *testing.T) {
	registry := mustRegistry(t)
	tool := registry.newActivateTool()
	if tool.Name != ActivateToolName || tool.Mode != droids.ModeParallel {
		t.Fatalf("tool identity = %q/%q", tool.Name, tool.Mode)
	}
	if tool.Parameters["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %#v, want false", tool.Parameters["additionalProperties"])
	}
	if !reflect.DeepEqual(tool.Parameters["required"], []string{"name"}) {
		t.Fatalf("required = %#v", tool.Parameters["required"])
	}
	properties, ok := tool.Parameters["properties"].(map[string]any)
	if !ok || len(properties) != 1 {
		t.Fatalf("properties = %#v", tool.Parameters["properties"])
	}
	name, ok := properties["name"].(map[string]any)
	if !ok || name["pattern"] != "^[a-z0-9]+(?:-[a-z0-9]+)*$" {
		t.Fatalf("name schema = %#v", properties["name"])
	}
	// Construction compiles the schema through droids' strict validator.
	_ = registry.ActivateTool()
}

func TestActivateSkillHonorsCancellation(t *testing.T) {
	tool := mustRegistry(t).newActivateTool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, droids.ToolContext{}, activateArgs{Name: KitCustomizationName}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("activation error = %v, want context canceled", err)
	}
}

func TestActivateSkillRunsThroughDroidAndPersistsDetails(t *testing.T) {
	registry := mustRegistry(t)
	providers := &activationProviders{arguments: []byte(`{"name":"kit-customization"}`)}
	droid, err := droids.Spawn(t.Context(), "conversation_skill_activation", droids.Config{
		Store: droids.NewMemoryStore(), Providers: providers, Model: "test/skills",
		Tools: []droids.AnyTool{registry.ActivateTool()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "Customize Kit."}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil || outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("Wait() = %+v, %v", outcome, err)
	}
	history, err := droid.History(t.Context(), droids.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	result := historyToolResult(t, history.Messages)
	if result.IsError || result.ToolName != ActivateToolName {
		t.Fatalf("tool result = %#v", result)
	}
	var details activateDetails
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details != (activateDetails{Name: KitCustomizationName, Found: true, Source: SourceBuiltin}) {
		t.Fatalf("persisted details = %#v", details)
	}
	if got := resultContentText(t, result.Content); !strings.Contains(got, "# Kit customization") {
		t.Fatalf("persisted activation content = %q", got)
	}
}

func TestActivateSkillSchemaRejectsAdditionalArgumentsThroughDroid(t *testing.T) {
	registry := mustRegistry(t)
	providers := &activationProviders{arguments: []byte(`{"name":"kit-customization","extra":true}`)}
	droid, err := droids.Spawn(t.Context(), "conversation_skill_invalid", droids.Config{
		Store: droids.NewMemoryStore(), Providers: providers, Model: "test/skills",
		Tools: []droids.AnyTool{registry.ActivateTool()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "Customize Kit."}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil || outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("Wait() = %+v, %v", outcome, err)
	}
	history, err := droid.History(t.Context(), droids.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	result := historyToolResult(t, history.Messages)
	if !result.IsError || len(result.Details) != 0 || !strings.Contains(resultContentText(t, result.Content), "additional properties 'extra' not allowed") {
		t.Fatalf("invalid-schema tool result = %#v", result)
	}
}

func mustRegistry(t *testing.T, additions ...Skill) *Registry {
	t.Helper()
	registry, err := NewRegistry(additions...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func toolResultText(t *testing.T, result droids.ToolResult) string {
	t.Helper()
	return resultContentText(t, result.Content)
}

func resultContentText(t *testing.T, content []droids.ResultContent) string {
	t.Helper()
	if len(content) != 1 {
		t.Fatalf("result content = %#v", content)
	}
	text, ok := content[0].(droids.TextContent)
	if !ok {
		t.Fatalf("result content type = %T", content[0])
	}
	return text.Text
}

func historyToolResult(t *testing.T, messages []droids.MessageEnvelope) droids.ToolResultMessage {
	t.Helper()
	for _, envelope := range messages {
		if result, ok := envelope.Message.(droids.ToolResultMessage); ok {
			return result
		}
	}
	t.Fatalf("history has no tool result: %#v", messages)
	return droids.ToolResultMessage{}
}

type activationProviders struct {
	mu        sync.Mutex
	arguments []byte
	requests  []droids.Request
}

func (p *activationProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *activationProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *activationProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "skills" || id == "test/skills"
}
func (p *activationProviders) RefreshModels(context.Context) error { return nil }
func (p *activationProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	call := len(p.requests)
	p.mu.Unlock()
	if call == 1 {
		return activationStream{message: droids.AssistantMessage{
			Provider: "test", Model: "skills", StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{droids.ToolCall{
				ID: "call_activate", Name: ActivateToolName, Arguments: append([]byte(nil), p.arguments...),
			}},
		}}
	}
	return activationStream{message: droids.AssistantMessage{
		Provider: "test", Model: "skills", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "Activated."}},
	}}
}
func (*activationProviders) model() droids.Model {
	return droids.Model{ID: "skills", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
}

type activationStream struct{ message droids.AssistantMessage }

func (s activationStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: s.message.Provider, Model: s.message.Model}}
	events <- droids.StreamDone{Message: s.message}
	close(events)
	return events
}
func (s activationStream) Result() droids.AssistantMessage { return s.message }
