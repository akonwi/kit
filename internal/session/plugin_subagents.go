package session

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/subagent"
)

// PluginSubagent is the session-facing projection of one generation-owned
// child-agent definition.
type PluginSubagent struct {
	ID, PluginID, Instance, Description, Instructions, Model, SourcePath string
	Registration                                                         uint64
}

// PluginSubagentHost exposes active definitions without plugin process IO.
type PluginSubagentHost interface{ Subagents() []PluginSubagent }

// PluginSubagentCatalogRegistry binds the authoritative applied runtime catalog
// into the existing parent subagent tool. The returned cleanup is generation-safe.
type PluginSubagentCatalogRegistry interface {
	RegisterPluginCatalogProvider(string, func() (subagent.Catalog, error)) (func(), error)
}

type pluginContributionState struct {
	mu          sync.Mutex
	fingerprint [32]byte
	base        subagent.LoadResult
	effective   subagent.LoadResult
	plugins     []PluginSubagent
	droid       atomic.Pointer[droids.Droid]
}

func (s *pluginContributionState) initialize(base subagent.LoadResult, droid *droids.Droid) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base = cloneSubagentLoadResult(base)
	s.effective = cloneSubagentLoadResult(base)
	s.plugins = nil
	s.droid.Store(droid)
}

func (s *pluginContributionState) setDroid(droid *droids.Droid) { s.droid.Store(droid) }

func (s *pluginContributionState) setBase(base subagent.LoadResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base = cloneSubagentLoadResult(base)
	s.effective = cloneSubagentLoadResult(base)
	s.plugins = nil
	s.fingerprint = [32]byte{}
}

func (r *runtime) appliedPluginSubagentCatalog() (subagent.Catalog, error) {
	r.pluginContributions.mu.Lock()
	applied := append([]PluginSubagent(nil), r.pluginContributions.plugins...)
	r.pluginContributions.mu.Unlock()
	host, ok := r.plugins.(PluginSubagentHost)
	if !ok {
		return subagent.NewCatalog()
	}
	type identity struct {
		instance     string
		registration uint64
	}
	active := make(map[string]identity)
	for _, definition := range host.Subagents() {
		active[definition.ID] = identity{definition.Instance, definition.Registration}
	}
	filtered := applied[:0]
	for _, definition := range applied {
		if active[definition.ID] == (identity{definition.Instance, definition.Registration}) {
			filtered = append(filtered, definition)
		}
	}
	definitions, _, err := projectPluginSubagents(filtered)
	if err != nil {
		return subagent.Catalog{}, err
	}
	return subagent.NewCatalog(definitions...)
}

func (s *pluginContributionState) snapshot() subagent.LoadResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSubagentLoadResult(s.effective)
}

func cloneSubagentLoadResult(value subagent.LoadResult) subagent.LoadResult {
	catalog, err := subagent.NewCatalog(value.Catalog.Definitions()...)
	if err == nil {
		value.Catalog = catalog
	}
	value.Diagnostics = append([]subagent.Diagnostic(nil), value.Diagnostics...)
	return value
}

func projectPluginSubagents(contributions []PluginSubagent) ([]subagent.Definition, string, error) {
	definitions := make([]subagent.Definition, 0, len(contributions))
	for _, contribution := range contributions {
		definitions = append(definitions, subagent.Definition{
			Name: contribution.ID, Description: contribution.Description, Instructions: contribution.Instructions, Model: contribution.Model,
			Source: subagent.Source{Kind: subagent.SourcePlugin, Path: contribution.SourcePath, PluginID: contribution.PluginID},
		})
	}
	catalog, err := subagent.NewCatalog(definitions...)
	if err != nil {
		return nil, "", err
	}
	section, err := catalog.CatalogSection()
	if err != nil {
		return nil, "", err
	}
	prompt := ""
	if section.Text != "" {
		prompt = "\n\n" + section.Text
	}
	return catalog.Definitions(), prompt, nil
}

func mergeSubagentCatalog(base subagent.LoadResult, pluginDefinitions []subagent.Definition) (subagent.LoadResult, error) {
	definitions := append(base.Catalog.Definitions(), pluginDefinitions...)
	catalog, err := subagent.NewCatalog(definitions...)
	if err != nil {
		return subagent.LoadResult{}, err
	}
	return subagent.LoadResult{Catalog: catalog, Diagnostics: append([]subagent.Diagnostic(nil), base.Diagnostics...)}, nil
}

func pluginContributionFingerprint(tools []PluginTool, subagents []PluginSubagent) [32]byte {
	raw, _ := json.Marshal(struct {
		Tools     []PluginTool     `json:"tools"`
		Subagents []PluginSubagent `json:"subagents"`
	}{tools, subagents})
	return sha256.Sum256(raw)
}

func pluginSubagentNames(result subagent.LoadResult) []string {
	definitions := result.Catalog.Definitions()
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func pluginContributionWarning(err error) string {
	return "Plugin contributions unavailable: " + boundedReloadWarning(fmt.Sprint(err))
}
