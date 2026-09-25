package plugin

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

const (
	// MaxSubagents bounds the effective filesystem and plugin definition catalog.
	MaxSubagents = 128
	// MaxSubagentInstructionsBytes is constrained further by the 64 KiB encoded
	// contribution-parameter bound.
	MaxSubagentInstructionsBytes = 128 * 1024
)

// Subagent is one immutable generation-owned child-agent definition.
type Subagent struct {
	ID, LocalID, Description, Instructions, Model, SourcePath string
	Owner                                                     InstanceID
	Registration                                              uint64
}

func parseSubagent(params json.RawMessage) (Subagent, error) {
	fields, err := contributionObject(params, "id", "description", "instructions", "model")
	if err != nil {
		return Subagent{}, err
	}
	var result Subagent
	if result.LocalID, err = contributionString(fields, "id", 128, true); err != nil || !localID(result.LocalID) {
		if err != nil {
			return Subagent{}, err
		}
		return Subagent{}, rpcError(-32602, "Invalid subagent id")
	}
	if result.Description, err = contributionString(fields, "description", 1024, true); err != nil {
		return Subagent{}, err
	}
	if result.Instructions, err = interactionString(fields, "instructions", MaxSubagentInstructionsBytes); err != nil {
		return Subagent{}, err
	}
	if strings.TrimSpace(result.Instructions) == "" {
		return Subagent{}, rpcError(-32602, "Subagent instructions are required")
	}
	if result.Model, err = contributionString(fields, "model", 256, false); err != nil {
		return Subagent{}, err
	}
	result.Description = strings.TrimSpace(result.Description)
	result.Instructions = strings.TrimSpace(result.Instructions)
	result.Model = strings.TrimSpace(result.Model)
	return result, nil
}

func (h *Host) handleSubagentRequest(ctx context.Context, owner InstanceID, method string, params json.RawMessage) (json.RawMessage, error) {
	if method == "kit/subagents/unregister" {
		localID, err := parseLocalID(params)
		if err != nil {
			return nil, err
		}
		canonical := owner.PluginID + "." + localID
		h.mu.Lock()
		defer h.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if h.activeEntryLocked(owner) == nil {
			return nil, rpcError(-32002, "Plugin generation is unavailable")
		}
		h.pruneSubagentsLocked()
		if current, ok := h.subagents[canonical]; ok && current.Owner == owner {
			delete(h.subagents, canonical)
			h.publishChange()
		}
		return json.RawMessage("null"), nil
	}

	definition, err := parseSubagent(params)
	if err != nil {
		return nil, err
	}
	definition.Owner = owner
	definition.ID = owner.PluginID + "." + definition.LocalID
	if len(definition.ID) > 128 {
		return nil, rpcError(-32602, "Canonical subagent id exceeds 128 bytes")
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry := h.activeEntryLocked(owner)
	if entry == nil {
		return nil, rpcError(-32002, "Plugin generation is unavailable")
	}
	h.pruneSubagentsLocked()
	if _, reserved := h.reservedSubagents[definition.ID]; reserved {
		return nil, rpcError(-32003, "Subagent conflicts with an existing definition")
	}
	if _, exists := h.subagents[definition.ID]; exists {
		return nil, rpcError(-32003, "Subagent is already registered")
	}
	if len(h.reservedSubagents)+len(h.subagents) >= MaxSubagents {
		return nil, rpcError(-32005, "Session subagent limit exceeded")
	}
	definition.SourcePath = entry.installation.ManifestPath
	if !contributionText(definition.SourcePath, 4*1024, true) {
		return nil, rpcError(-32005, "Subagent source location exceeds protocol limits")
	}
	h.nextSubagent++
	definition.Registration = h.nextSubagent
	h.subagents[definition.ID] = definition
	h.publishChange()
	return json.Marshal(struct {
		ID string `json:"id"`
	}{definition.ID})
}

func (h *Host) pruneSubagentsLocked() {
	for id, definition := range h.subagents {
		if h.activeEntryLocked(definition.Owner) == nil {
			delete(h.subagents, id)
		}
	}
}

// Subagents returns active definitions in stable canonical-name order.
func (h *Host) Subagents() []Subagent {
	h.mu.Lock()
	defer h.mu.Unlock()
	var result []Subagent
	for _, definition := range h.subagents {
		if h.activeEntryLocked(definition.Owner) != nil {
			result = append(result, definition)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// SetSubagentBase replaces names reserved by the currently applied filesystem
// catalog. Base definitions take precedence over active plugin contributions.
func (h *Host) SetSubagentBase(names []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	next := make(map[string]struct{}, len(names))
	for _, name := range names {
		next[name] = struct{}{}
	}
	h.reservedSubagents = next
	changed := false
	for id, definition := range h.subagents {
		if _, conflict := next[id]; conflict {
			delete(h.subagents, id)
			h.recordDiagnosticLocked(definition.Owner.PluginID+": subagent "+id+" conflicts with an applied filesystem definition", false)
			changed = true
		}
	}
	ids := make([]string, 0, len(h.subagents))
	for id := range h.subagents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	allowed := MaxSubagents - len(next)
	if allowed < 0 {
		allowed = 0
	}
	if allowed > len(ids) {
		allowed = len(ids)
	}
	for _, id := range ids[allowed:] {
		definition := h.subagents[id]
		delete(h.subagents, id)
		h.recordDiagnosticLocked(definition.Owner.PluginID+": subagent "+id+" was removed because the applied catalog reached its definition limit", false)
		changed = true
	}
	if changed {
		h.publishChange()
	}
}
