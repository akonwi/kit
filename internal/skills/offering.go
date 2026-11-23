package skills

import (
	"strings"
	"sync"
)

// Offering is one agent's view of a registry. A skill that needs client
// tools is offered, which means it's listed and activate_skill loads it, only
// while every tool it needs is connected. Kit offers every other skill
// throughout. An Offering is safe for concurrent use; a nil Offering offers
// no skill that needs client tools.
//
// An agent changes its client tools in two steps, so activation never
// offers a skill its tools and prompt don't: Prepare before installing the
// new tools and their prompt, then SetClientTools once they're installed.
type Offering struct {
	registry *Registry
	mu       sync.RWMutex
	// connected holds the client tools connected now.
	connected map[string]struct{}
}

// NewOffering returns an offering of r with no client tools connected.
func (r *Registry) NewOffering() *Offering {
	return &Offering{registry: r}
}

// Prepare withdraws the skills that need a tool missing from names, the
// client-hosted tools about to be connected, by model name. It returns the
// prompt text listing the skills those tools make available, or empty when
// they make none available.
func (o *Offering) Prepare(names []string) string {
	if o == nil {
		return ""
	}
	next := toolSet(names)
	o.mu.Lock()
	for name := range o.connected {
		if _, ok := next[name]; !ok {
			delete(o.connected, name)
		}
	}
	o.mu.Unlock()
	return clientToolCatalog(conditionalSkills(o.registry, next))
}

// SetClientTools records the client-hosted tools connected now, by model
// name, once the tools and the prompt Prepare returned are installed.
func (o *Offering) SetClientTools(names []string) {
	if o == nil {
		return
	}
	connected := toolSet(names)
	o.mu.Lock()
	o.connected = connected
	o.mu.Unlock()
}

// Offers reports whether activate_skill loads the named skill now.
func (o *Offering) Offers(name string) bool {
	if o == nil {
		return false
	}
	skill, ok := o.registry.Lookup(name)
	return ok && o.offered(skill)
}

// offered reports whether the agent may activate skill now.
func (o *Offering) offered(skill Skill) bool {
	if len(skill.ClientTools) == 0 {
		return true
	}
	if o == nil {
		return false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return connects(o.connected, skill)
}

// offeredSkills returns the model-invocable skills offered now.
func (o *Offering) offeredSkills() []Skill {
	skills := o.registry.modelInvocableSkills()
	offered := skills[:0]
	for _, skill := range skills {
		if o.offered(skill) {
			offered = append(offered, skill)
		}
	}
	return offered
}

// conditionalSkills returns the model-invocable skills of r that need client
// tools, all of which are in connected, or all of them when connected is nil.
func conditionalSkills(r *Registry, connected map[string]struct{}) []Skill {
	var result []Skill
	for _, skill := range r.modelInvocableSkills() {
		if len(skill.ClientTools) > 0 && (connected == nil || connects(connected, skill)) {
			result = append(result, skill)
		}
	}
	return result
}

// clientToolCatalog is the prompt text listing skills that client tools make
// available, or empty for none.
func clientToolCatalog(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var catalog strings.Builder
	catalog.WriteString("\n\nConnected client tools make these skills available too, through activate_skill:\n")
	writeSkillList(&catalog, skills)
	return catalog.String()
}

func toolSet(names []string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

func connects(connected map[string]struct{}, skill Skill) bool {
	for _, name := range skill.ClientTools {
		if _, ok := connected[name]; !ok {
			return false
		}
	}
	return true
}
