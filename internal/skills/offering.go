package skills

import (
	"strings"
	"sync"
)

// Offering is one agent's view of a registry. A skill that needs client
// tools is offered, which means it's listed and activate_skill loads it, only
// while every tool it needs is connected. Kit offers every other skill
// throughout. An Offering is safe for concurrent use.
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

// SetClientTools records the client-hosted tools connected now, by model
// name, and returns the prompt text listing the skills they make available,
// or empty when they make none available. A nil Offering offers nothing.
func (o *Offering) SetClientTools(names []string) string {
	if o == nil {
		return ""
	}
	connected := make(map[string]struct{}, len(names))
	for _, name := range names {
		connected[name] = struct{}{}
	}
	o.mu.Lock()
	o.connected = connected
	o.mu.Unlock()
	var added []Skill
	for _, skill := range o.registry.modelInvocableSkills() {
		if len(skill.ClientTools) > 0 && o.offered(skill) {
			added = append(added, skill)
		}
	}
	if len(added) == 0 {
		return ""
	}
	var catalog strings.Builder
	catalog.WriteString("\n\nConnected client tools make these skills available too, through activate_skill:\n")
	writeSkillList(&catalog, added)
	return catalog.String()
}

// offered reports whether the agent may activate skill now.
func (o *Offering) offered(skill Skill) bool {
	if len(skill.ClientTools) == 0 {
		return true
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	for _, name := range skill.ClientTools {
		if _, ok := o.connected[name]; !ok {
			return false
		}
	}
	return true
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
