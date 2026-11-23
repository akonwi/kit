package session

import (
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/subagent"
)

func TestReplacingTheBundleWithdrawsTheReplacedBrowserSkill(t *testing.T) {
	root := t.TempDir()
	loader, err := skills.NewFilesystemLoader(apphome.FromHome(filepath.Join(root, "kit-home")))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loader.Load(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	browserTools := []string{"browser__tabs", "browser__navigate", "browser__snapshot", "browser__act", "browser__screenshot"}
	replaced, replacement := loaded.Registry.NewOffering(), loaded.Registry.NewOffering()
	var state pluginContributionState
	state.initialize(subagent.LoadResult{}, replaced, nil)
	replaced.SetClientTools(browserTools)
	if !replaced.Offers(skills.KitBrowserName) {
		t.Fatal("the connected offering doesn't offer kit-browser")
	}

	// A request still running on the replaced tools calls the replaced
	// activate_skill, which no refresh reaches again.
	state.setBase(subagent.LoadResult{}, replacement)
	if replaced.Offers(skills.KitBrowserName) {
		t.Fatal("the replaced offering still offers kit-browser")
	}
}
