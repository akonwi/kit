package skills

import (
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/systemprompt"
)

const catalogSectionID = "kit.skills.catalog"

// CatalogSection returns the model-visible catalog for this immutable registry.
func (r *Registry) CatalogSection() (systemprompt.Section, error) {
	skills := r.modelInvocableSkills()
	if len(skills) == 0 {
		return systemprompt.Section{}, fmt.Errorf("skill registry requires the embedded customization skill")
	}

	var catalog strings.Builder
	catalog.WriteString("The following skills provide specialized instructions for specific tasks.\n")
	catalog.WriteString("Call the activate_skill tool with the skill name to activate it when the task matches its description.\n")
	catalog.WriteString("When a skill's instructions reference a relative path, resolve it against the skill directory and use that absolute path in tool commands.\n\n")
	catalog.WriteString("<available_skills>\n")
	sources := make([]systemprompt.Source, 0, len(skills))
	for _, skill := range skills {
		catalog.WriteString("  <skill>\n")
		catalog.WriteString("    <name>")
		catalog.WriteString(escapeXML(skill.Name))
		catalog.WriteString("</name>\n")
		catalog.WriteString("    <description>")
		catalog.WriteString(escapeXML(skill.Description))
		catalog.WriteString("</description>\n")
		catalog.WriteString("    <source>")
		catalog.WriteString(escapeXML(string(skill.Source)))
		catalog.WriteString("</source>\n")
		if skill.Location != "" {
			catalog.WriteString("    <location>")
			catalog.WriteString(escapeXML(skill.Location))
			catalog.WriteString("</location>\n")
		}
		catalog.WriteString("  </skill>\n")
		sources = append(sources, systemprompt.Source{ID: "skill:" + skill.Name, Path: skill.Location})
	}
	catalog.WriteString("</available_skills>")
	if catalog.Len() > maxSkillCatalogBytes {
		return systemprompt.Section{}, fmt.Errorf("skill catalog exceeds the %d-byte limit", maxSkillCatalogBytes)
	}
	return systemprompt.Section{
		ID:      catalogSectionID,
		Kind:    systemprompt.SectionSkillCatalog,
		Text:    catalog.String(),
		Sources: sources,
	}, nil
}

func escapeXML(value string) string {
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
