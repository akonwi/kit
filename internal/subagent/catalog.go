package subagent

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxDefinitions      = 128
	maxNameBytes        = 128
	maxDescriptionBytes = 1024
	maxModelBytes       = 256
	maxInstructions     = 128 << 10
	maxLocationBytes    = 4 << 10
)

// Catalog is one session's deterministic immutable definition snapshot.
type Catalog struct {
	definitions []Definition
	byName      map[string]Definition
}

// NewCatalog validates definitions and orders them by name.
func NewCatalog(definitions ...Definition) (Catalog, error) {
	if len(definitions) > maxDefinitions {
		return Catalog{}, fmt.Errorf("subagent catalog exceeds %d definitions", maxDefinitions)
	}
	all := append([]Definition(nil), definitions...)
	byName := make(map[string]Definition, len(all))
	for index, definition := range all {
		normalized, err := normalizeDefinition(definition)
		if err != nil {
			return Catalog{}, err
		}
		if _, exists := byName[normalized.Name]; exists {
			return Catalog{}, fmt.Errorf("subagent %q is duplicated", normalized.Name)
		}
		all[index] = normalized
		byName[normalized.Name] = normalized
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return Catalog{definitions: all, byName: byName}, nil
}

// Definitions returns all definitions in stable name order.
func (c Catalog) Definitions() []Definition {
	return append([]Definition(nil), c.definitions...)
}

// Lookup returns one definition by exact name.
func (c Catalog) Lookup(name string) (Definition, bool) {
	definition, ok := c.byName[name]
	return definition, ok
}

// Len returns the number of definitions.
func (c Catalog) Len() int { return len(c.definitions) }

func normalizeDefinition(definition Definition) (Definition, error) {
	definition.Name = strings.TrimSpace(definition.Name)
	definition.Description = strings.TrimSpace(definition.Description)
	definition.Model = strings.TrimSpace(definition.Model)
	definition.Instructions = strings.TrimSpace(definition.Instructions)
	if definition.Name == "" || len(definition.Name) > maxNameBytes || !validName(definition.Name) {
		return Definition{}, errors.New("subagent name must be 1-128 visible non-whitespace characters")
	}
	if definition.Description == "" || len(definition.Description) > maxDescriptionBytes || !validRendererText(definition.Description) {
		return Definition{}, fmt.Errorf("subagent %q has an invalid description", definition.Name)
	}
	if definition.Model != "" && (len(definition.Model) > maxModelBytes || !validRendererText(definition.Model)) {
		return Definition{}, fmt.Errorf("subagent %q has an invalid model identifier", definition.Name)
	}
	if definition.Instructions == "" || len(definition.Instructions) > maxInstructions || !validText(definition.Instructions) {
		return Definition{}, fmt.Errorf("subagent %q has invalid instructions", definition.Name)
	}
	if definition.Source.Kind != SourceUser && definition.Source.Kind != SourceProject && definition.Source.Kind != SourcePlugin {
		return Definition{}, fmt.Errorf("subagent %q has an invalid source", definition.Name)
	}
	if definition.Source.Kind == SourcePlugin {
		if definition.Source.PluginID == "" || !validRendererText(definition.Source.PluginID) {
			return Definition{}, fmt.Errorf("subagent %q has an invalid plugin source", definition.Name)
		}
	} else if definition.Source.PluginID != "" {
		return Definition{}, fmt.Errorf("subagent %q has plugin identity on a filesystem source", definition.Name)
	}
	if definition.Source.Path == "" || len(definition.Source.Path) > maxLocationBytes || !validRendererText(definition.Source.Path) {
		return Definition{}, fmt.Errorf("subagent %q has an invalid source path", definition.Name)
	}
	return definition, nil
}

func validName(value string) bool {
	if !validRendererText(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsSpace(character) || character == '/' || character == '\\' {
			return false
		}
	}
	return true
}

func validText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validRendererText(value string) bool {
	if !validText(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}
