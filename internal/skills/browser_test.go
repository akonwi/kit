package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/systemprompt"
)

// browserTools are the tools the Kit app's browser plugin connects.
var browserTools = []string{"browser__tabs", "browser__navigate", "browser__snapshot", "browser__act", "browser__screenshot"}

func browserRegistry(t *testing.T, cwd string, paths apphome.Paths) LoadResult {
	t.Helper()
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := rexLoader(t, paths, "").Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func activate(t *testing.T, offering *Offering, name string) (string, activateDetails) {
	t.Helper()
	result, err := offering.newActivateTool().Execute(context.Background(), droids.ToolContext{}, activateArgs{Name: name}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var details activateDetails
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	if result.IsError == details.Found {
		t.Fatalf("activation IsError = %v with details %#v", result.IsError, details)
	}
	return toolResultText(t, result), details
}

// connect connects names to offering as an agent does, and returns the
// prompt text that lists the skills they make available.
func connect(offering *Offering, names []string) string {
	prompt := offering.Prepare(names)
	offering.SetClientTools(names)
	return prompt
}

func TestTheBrowserSkillIsListedWhileTheBrowserToolsAreConnected(t *testing.T) {
	base := t.TempDir()
	result := browserRegistry(t, filepath.Join(base, "project"), apphome.FromHome(filepath.Join(base, "kit-home")))
	browser, ok := result.Registry.Lookup(KitBrowserName)
	if !ok || browser.Source != SourceBuiltin || !strings.HasPrefix(browser.Content, "---\nname: kit-browser\n") {
		t.Fatalf("browser skill = %#v", browser)
	}
	section, err := result.Registry.CatalogSection()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := catalogNames(section.Text), []string{KitCustomizationName, KitFiguresName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog names = %#v, want %#v", got, want)
	}

	offering := result.Registry.NewOffering()
	want := "\n\nConnected client tools make these skills available too, through activate_skill:\n" +
		"<available_skills>\n  <skill>\n    <name>kit-browser</name>\n    <description>" + escapeXML(browser.Description) +
		"</description>\n    <source>built-in</source>\n  </skill>\n</available_skills>"
	if got := connect(offering, append([]string{"other__tool"}, browserTools...)); got != want {
		t.Fatalf("connected catalog = %q, want %q", got, want)
	}
	text, details := activate(t, offering, KitBrowserName)
	if text != browser.Content || details != (activateDetails{Name: KitBrowserName, Found: true, Source: SourceBuiltin}) {
		t.Fatalf("connected activation = %q %#v", text, details)
	}

	// Disconnecting, or losing any one of the tools, withdraws the skill.
	for _, connected := range [][]string{nil, browserTools[:4]} {
		if got := connect(offering, connected); got != "" {
			t.Fatalf("catalog with %v = %q, want none", connected, got)
		}
		text, details = activate(t, offering, KitBrowserName)
		wantText := `Skill "kit-browser" isn't available now: it needs the tools browser__act, browser__navigate, browser__screenshot, browser__snapshot, browser__tabs, which a connected Kit app provides, and they aren't connected.`
		if text != wantText || details != (activateDetails{Name: KitBrowserName}) {
			t.Fatalf("activation with %v = %q %#v", connected, text, details)
		}
		text, _ = activate(t, offering, "missing")
		if want := `Unknown skill "missing". Available skills: kit-customization, kit-figures.`; text != want {
			t.Fatalf("unknown activation = %q, want %q", text, want)
		}
	}

	// Reconnecting offers it again.
	if got := connect(offering, browserTools); got != want {
		t.Fatalf("reconnected catalog = %q, want %q", got, want)
	}
	if text, _ = activate(t, offering, "missing"); text != `Unknown skill "missing". Available skills: kit-browser, kit-customization, kit-figures.` {
		t.Fatalf("unknown activation while connected = %q", text)
	}
}

func TestActivationOffersTheBrowserSkillOnlyOnceItsToolsAreInstalled(t *testing.T) {
	base := t.TempDir()
	result := browserRegistry(t, filepath.Join(base, "project"), apphome.FromHome(filepath.Join(base, "kit-home")))
	offering := result.Registry.NewOffering()
	unavailable := func() bool {
		_, details := activate(t, offering, KitBrowserName)
		return !details.Found
	}

	if prompt := offering.Prepare(browserTools); !strings.Contains(prompt, "<name>kit-browser</name>") || !unavailable() {
		t.Fatalf("after Prepare: prompt %q, available %v", prompt, !unavailable())
	}
	offering.SetClientTools(browserTools)
	if unavailable() {
		t.Fatal("the skill is unavailable once its tools are installed")
	}
	// Preparing to remove a tool withdraws the skill before the tools change.
	if prompt := offering.Prepare(browserTools[1:]); prompt != "" || !unavailable() {
		t.Fatalf("after preparing to remove a tool: prompt %q, available %v", prompt, !unavailable())
	}
	var none *Offering
	if prompt := none.Prepare(browserTools); prompt != "" {
		t.Fatalf("nil offering prompt = %q", prompt)
	}
	none.SetClientTools(browserTools)
}

func TestTheCatalogLimitCountsSkillsThatClientToolsMakeAvailable(t *testing.T) {
	// fill builds a registry whose static catalog is exactly size bytes.
	fill := func(size int) *Registry {
		additions := make([]Skill, 0, 64)
		for index := 0; index < cap(additions); index++ {
			additions = append(additions, Skill{
				Name: fmt.Sprintf("skill-%d", index), Description: strings.Repeat("d", maxDescriptionBytes),
				Content: "content", Source: SourceProject, Location: "/" + strings.Repeat("l", maxSkillLocationBytes-1),
			})
		}
		registry, err := newRegistry([]Skill{kitBrowserSkill()}, additions)
		if err != nil {
			t.Fatal(err)
		}
		var static []Skill
		for _, skill := range registry.Skills() {
			if len(skill.ClientTools) == 0 {
				static = append(static, skill)
			}
		}
		var catalog strings.Builder
		writeSkillList(&catalog, static)
		// The catalog's three-line preamble.
		preamble := 0
		for _, line := range []string{
			"The following skills provide specialized instructions for specific tasks.\n",
			"Call the activate_skill tool with the skill name to activate it when the task matches its description.\n",
			"When a skill's instructions reference a relative path, resolve it against the skill directory and use that absolute path in tool commands.\n\n",
		} {
			preamble += len(line)
		}
		excess := preamble + catalog.Len() - size
		for index := range additions {
			cut := min(excess, len(additions[index].Location)-1)
			additions[index].Location = additions[index].Location[:len(additions[index].Location)-cut]
			excess -= cut
		}
		if excess != 0 {
			t.Fatalf("could not trim the catalog to %d bytes", size)
		}
		registry, err = newRegistry([]Skill{kitBrowserSkill()}, additions)
		if err != nil {
			t.Fatal(err)
		}
		return registry
	}
	browser := len(clientToolCatalog([]Skill{kitBrowserSkill()}))
	if _, err := fill(maxSkillCatalogBytes - browser).CatalogSection(); err != nil {
		t.Fatalf("CatalogSection() rejected a catalog at the limit: %v", err)
	}
	if _, err := fill(maxSkillCatalogBytes - browser + 1).CatalogSection(); err == nil {
		t.Fatal("CatalogSection() accepted a catalog over the limit with the browser skill listed")
	}
}

func TestYourOwnKitBrowserSkillReplacesKits(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	own := writeSkill(t, paths.Skills, KitBrowserName, "My browser notes", "my body", false)
	result := browserRegistry(t, filepath.Join(base, "project"), paths)
	browser, ok := result.Registry.Lookup(KitBrowserName)
	if !ok || browser.Source != SourceUser || browser.Location != own || len(browser.ClientTools) != 0 {
		t.Fatalf("browser skill = %#v", browser)
	}
	want := []systemprompt.Diagnostic{{
		Severity: systemprompt.DiagnosticInfo, Code: "skills.builtin_replaced",
		Message: "Kit's built-in browser skill was replaced by this skill",
		Source:  result.Diagnostics[0].Source,
	}}
	if !reflect.DeepEqual(result.Diagnostics, want) || result.Diagnostics[0].Source.Path != own {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	section, err := result.Registry.CatalogSection()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := catalogNames(section.Text), []string{KitBrowserName, KitCustomizationName, KitFiguresName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog names = %#v, want %#v", got, want)
	}
	offering := result.Registry.NewOffering()
	if got := connect(offering, browserTools); got != "" {
		t.Fatalf("connected catalog = %q, want none", got)
	}
	if text, details := activate(t, offering, KitBrowserName); !strings.Contains(text, "my body") || details.Source != SourceUser {
		t.Fatalf("activation = %q %#v", text, details)
	}
}

func TestOnlyEmbeddedSkillsMayNeedClientTools(t *testing.T) {
	_, err := NewRegistry(Skill{Name: "mine", Description: "Mine", Content: "Body", Source: SourceUser, ClientTools: []string{"browser__tabs"}})
	if err == nil || err.Error() != `skill "mine": only embedded skills may need client tools` {
		t.Fatalf("NewRegistry() error = %v", err)
	}
}

// catalogNames returns the skill names a catalog lists, in order.
func catalogNames(catalog string) []string {
	var names []string
	for _, line := range strings.Split(catalog, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "<name>"); ok {
			names = append(names, strings.TrimSuffix(name, "</name>"))
		}
	}
	return names
}
