package subagent

import (
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/systemprompt"
)

const (
	catalogSectionID      = "kit.subagents.catalog"
	maxPromptCatalogBytes = 256 << 10
)

// CatalogSection returns concise model-visible guidance and metadata for the
// configured subagents. Runtime and persistence identities are intentionally
// omitted.
func (c Catalog) CatalogSection() (systemprompt.Section, error) {
	definitions := c.Definitions()
	if len(definitions) == 0 {
		return systemprompt.Section{ID: catalogSectionID, Kind: systemprompt.SectionFeature}, nil
	}

	var catalog strings.Builder
	catalog.WriteString("The following named subagents are available for delegated work.\n")
	catalog.WriteString("Use the subagent tool when a task matches an agent's specialization. Starting work is asynchronous; continue useful parent work instead of immediately waiting unless the result is required.\n\n")
	catalog.WriteString("<available_subagents>\n")
	sources := make([]systemprompt.Source, 0, len(definitions))
	for _, definition := range definitions {
		catalog.WriteString("  <subagent>\n")
		catalog.WriteString("    <name>")
		catalog.WriteString(escapePromptXML(definition.Name))
		catalog.WriteString("</name>\n")
		catalog.WriteString("    <description>")
		catalog.WriteString(escapePromptXML(definition.Description))
		catalog.WriteString("</description>\n")
		catalog.WriteString("  </subagent>\n")
		sources = append(sources, systemprompt.Source{ID: "subagent:" + definition.Name, Path: definition.Source.Path})
	}
	catalog.WriteString("</available_subagents>")
	if catalog.Len() > maxPromptCatalogBytes {
		return systemprompt.Section{}, fmt.Errorf("subagent prompt catalog exceeds the %d-byte limit", maxPromptCatalogBytes)
	}
	return systemprompt.Section{
		ID:      catalogSectionID,
		Kind:    systemprompt.SectionFeature,
		Text:    catalog.String(),
		Sources: sources,
	}, nil
}

func escapePromptXML(value string) string {
	var escaped strings.Builder
	for _, character := range value {
		switch character {
		case '&':
			escaped.WriteString("&amp;")
		case '<':
			escaped.WriteString("&lt;")
		case '>':
			escaped.WriteString("&gt;")
		case '"':
			escaped.WriteString("&quot;")
		case '\'':
			escaped.WriteString("&apos;")
		default:
			escaped.WriteRune(character)
		}
	}
	return escaped.String()
}
