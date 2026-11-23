package skills

import (
	"context"
	"encoding/json"
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
	if got := offering.SetClientTools(append([]string{"other__tool"}, browserTools...)); got != want {
		t.Fatalf("connected catalog = %q, want %q", got, want)
	}
	text, details := activate(t, offering, KitBrowserName)
	if text != browser.Content || details != (activateDetails{Name: KitBrowserName, Found: true, Source: SourceBuiltin}) {
		t.Fatalf("connected activation = %q %#v", text, details)
	}

	// Disconnecting, or losing any one of the tools, withdraws the skill.
	for _, connected := range [][]string{nil, browserTools[:4]} {
		if got := offering.SetClientTools(connected); got != "" {
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
	if got := offering.SetClientTools(browserTools); got != want {
		t.Fatalf("reconnected catalog = %q, want %q", got, want)
	}
	if text, _ = activate(t, offering, "missing"); text != `Unknown skill "missing". Available skills: kit-browser, kit-customization, kit-figures.` {
		t.Fatalf("unknown activation while connected = %q", text)
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
	if got := offering.SetClientTools(browserTools); got != "" {
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
