// Package skills owns Kit's model-activatable instruction registry.
package skills

import (
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// KitCustomizationName is the reserved identity of Kit's embedded customization skill.
	KitCustomizationName = "kit-customization"
	// KitFiguresName is the reserved identity of Kit's embedded figure-grammar skill.
	KitFiguresName               = "kit-figures"
	maxSkillNameBytes            = 64
	maxDescriptionBytes          = 1024
	maxSkillContentBytes         = 256 << 10
	maxSkillLocationBytes        = 4 << 10
	maxSkillsPerRegistry         = 128
	maxSkillCatalogBytes         = 256 << 10
	maxUnknownSkillResponseBytes = 16 << 10
)

var (
	// ErrReservedName means a non-built-in skill attempted to use a built-in identity.
	ErrReservedName = errors.New("skill name is reserved")
	// ErrDuplicateName means a skill identity occurs more than once.
	ErrDuplicateName = errors.New("skill name is duplicated")
)

// Embedded skill definitions use the same format as discovered SKILL.md
// files: YAML frontmatter with name and description, then instructions.
var (
	//go:embed kit-customization.md
	kitCustomizationDefinition string
	//go:embed kit-figures.md
	kitFiguresDefinition string
)

// Source identifies who owns a skill definition.
type Source string

const (
	// SourceBuiltin identifies a skill embedded in Kit's executable.
	SourceBuiltin Source = "built-in"
	// SourceUser identifies a skill discovered from the user's Kit home.
	SourceUser Source = "user"
	// SourceProject identifies a skill discovered from a session project.
	SourceProject Source = "project"
	// SourcePlugin identifies a skill contributed by a session plugin.
	SourcePlugin Source = "plugin"
)

// Skill is one model-activatable instruction bundle.
type Skill struct {
	Name                   string
	Description            string
	Content                string
	Source                 Source
	Location               string
	DisableModelInvocation bool
}

// Registry is the immutable set of skills applicable to one session. Skill
// changes create a replacement registry during a quiescent session reload so
// the catalog and activation tool always share one snapshot.
type Registry struct {
	skills []Skill
	byName map[string]Skill
}

// NewRegistry constructs a complete registry from Kit's embedded skills and
// ordered session-specific additions. Any collision rejects the complete set.
func NewRegistry(additions ...Skill) (*Registry, error) {
	builtins := builtinSkills()
	if len(additions)+len(builtins) > maxSkillsPerRegistry {
		return nil, fmt.Errorf("skill registry exceeds the %d-skill limit", maxSkillsPerRegistry)
	}
	all := make([]Skill, 0, len(additions)+len(builtins))
	all = append(all, builtins...)
	all = append(all, additions...)
	byName := make(map[string]Skill, len(all))
	for index, skill := range all {
		normalized, err := normalizeSkill(skill)
		if err != nil {
			return nil, err
		}
		if index >= len(builtins) && normalized.Source == SourceBuiltin {
			return nil, errors.New("callers cannot register embedded skills")
		}
		if index >= len(builtins) && IsReservedName(normalized.Name) {
			return nil, fmt.Errorf("%w: %s", ErrReservedName, normalized.Name)
		}
		if _, exists := byName[normalized.Name]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateName, normalized.Name)
		}
		byName[normalized.Name] = normalized
		all[index] = normalized
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return &Registry{skills: all, byName: byName}, nil
}

// Lookup returns a copy of the named skill.
func (r *Registry) Lookup(name string) (Skill, bool) {
	if r == nil {
		return Skill{}, false
	}
	skill, ok := r.byName[name]
	return skill, ok
}

// Skills returns all registered skills ordered by stable name.
func (r *Registry) Skills() []Skill {
	if r == nil {
		return nil
	}
	return append([]Skill(nil), r.skills...)
}

func (r *Registry) modelInvocableSkills() []Skill {
	skills := r.Skills()
	visible := skills[:0]
	for _, skill := range skills {
		if !skill.DisableModelInvocation {
			visible = append(visible, skill)
		}
	}
	return visible
}

func normalizeSkill(skill Skill) (Skill, error) {
	skill.Name = strings.TrimSpace(skill.Name)
	skill.Description = strings.TrimSpace(skill.Description)
	skill.Content = strings.TrimSpace(skill.Content)
	if skill.Name == "" || len(skill.Name) > maxSkillNameBytes || !validSkillName(skill.Name) {
		return Skill{}, fmt.Errorf("skill name must be 1-%d bytes of lowercase letters, digits, and single hyphens", maxSkillNameBytes)
	}
	if skill.Description == "" || len(skill.Description) > maxDescriptionBytes || !validModelText(skill.Description) {
		return Skill{}, fmt.Errorf("skill %q description must be non-empty valid text and at most %d bytes", skill.Name, maxDescriptionBytes)
	}
	if skill.Content == "" || len(skill.Content) > maxSkillContentBytes || !validModelText(skill.Content) {
		return Skill{}, fmt.Errorf("skill %q content must be non-empty valid text and at most %d bytes", skill.Name, maxSkillContentBytes)
	}
	if !validSource(skill.Source) {
		return Skill{}, fmt.Errorf("skill %q has invalid source %q", skill.Name, skill.Source)
	}
	if len(skill.Location) > maxSkillLocationBytes || !validModelText(skill.Location) {
		return Skill{}, fmt.Errorf("skill %q location must be valid text and at most %d bytes", skill.Name, maxSkillLocationBytes)
	}
	return skill, nil
}

func validSkillName(name string) bool {
	if name[0] == '-' || name[len(name)-1] == '-' || strings.Contains(name, "--") {
		return false
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validSource(source Source) bool {
	return source == SourceBuiltin || source == SourceUser || source == SourceProject || source == SourcePlugin
}

func validModelText(value string) bool {
	if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return false
	}
	for _, character := range value {
		if character == '\t' || character == '\n' || character == '\r' ||
			character >= 0x20 && character <= 0xd7ff ||
			character >= 0xe000 && character <= 0xfffd ||
			character >= 0x10000 && character <= 0x10ffff {
			continue
		}
		return false
	}
	return true
}

// IsReservedName reports whether name belongs to a skill embedded in Kit, which
// no user, project, or plugin skill may use.
func IsReservedName(name string) bool {
	return name == KitCustomizationName || name == KitFiguresName
}

// builtinSkills returns the skills embedded in Kit's executable.
func builtinSkills() []Skill {
	return []Skill{
		embeddedSkill(KitCustomizationName, kitCustomizationDefinition),
		embeddedSkill(KitFiguresName, kitFiguresDefinition),
	}
}

// embeddedSkill reads an embedded definition as discovery reads a SKILL.md
// file. Embedded definitions are compiled in, so one whose frontmatter is
// malformed or names another skill is a build defect, and panics.
func embeddedSkill(name, definition string) Skill {
	metadata, err := parseSkillFrontmatter(definition)
	if err != nil || metadata.Name != name || metadata.Description == "" {
		panic(fmt.Sprintf("embedded skill %q has invalid frontmatter (name %q): %v", name, metadata.Name, err))
	}
	return Skill{Name: name, Description: metadata.Description, Content: definition, Source: SourceBuiltin}
}
