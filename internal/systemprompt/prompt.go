// Package systemprompt owns Kit's model-facing system prompt composition.
package systemprompt

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// DefaultCore is the stable system prompt used by every Kit session unless an
// application composition root supplies an explicit alternative.
const DefaultCore = `You are Kit, a coding assistant running in the terminal.
You have access to tools to read and modify files, run commands, search code, and more.
Be concise and direct. Prefer surgical edits over full rewrites when practical.`

// SectionKind determines where a contribution appears in the effective prompt.
type SectionKind uint8

const (
	// SectionCore identifies the core prompt. Request-scoped sections cannot
	// replace it.
	SectionCore SectionKind = iota
	// SectionFeature contains guidance owned by an available built-in feature.
	SectionFeature
	// SectionSkillCatalog contains the available-skill catalog and activation guidance.
	SectionSkillCatalog
	// SectionPlugin contains one stable session-plugin prompt contribution.
	SectionPlugin
	// SectionContext contains session-scoped filesystem context.
	SectionContext
)

// Request identifies the session environment used to build a prompt.
type Request struct {
	SessionID string
	CWD       string
}

// Source describes one ordered input included in the effective prompt.
type Source struct {
	SectionID string
	ID        string
	Kind      SectionKind
	Path      string
}

// DiagnosticSeverity classifies a non-fatal prompt-building diagnostic.
type DiagnosticSeverity string

const (
	// DiagnosticInfo reports prompt-building information that needs no intervention.
	DiagnosticInfo DiagnosticSeverity = "info"
	// DiagnosticWarning reports guidance that could not be applied as intended.
	DiagnosticWarning DiagnosticSeverity = "warning"
)

// Diagnostic describes a structured, non-fatal prompt-building condition.
type Diagnostic struct {
	Severity DiagnosticSeverity
	Code     string
	Message  string
	Source   Source
}

// Section is one named, replaceable prompt contribution. Its slices are copied
// when the section is registered or supplied to BuildWith.
type Section struct {
	ID          string
	Kind        SectionKind
	Text        string
	Sources     []Source
	Diagnostics []Diagnostic
}

// Builder assembles the effective system prompt for one session.
type Builder interface {
	Build(context.Context, Request) (Result, error)
}

// Result is one complete system prompt and its ordered provenance.
type Result struct {
	Prompt      string
	Sources     []Source
	Diagnostics []Diagnostic
}

// Composer assembles an immutable core prompt with request-scoped sections.
// A Composer is safe for concurrent use because construction completes all of
// its state; BuildWith never mutates it.
type Composer struct {
	core string
}

type sectionKey struct {
	kind SectionKind
	id   string
}

var _ Builder = (*Composer)(nil)

// New constructs a Composer around a non-empty core prompt.
func New(core string) (*Composer, error) {
	core = strings.TrimSpace(core)
	if core == "" {
		return nil, errors.New("system prompt core is required")
	}
	return &Composer{core: core}, nil
}

// NewDefault constructs a Composer using DefaultCore.
func NewDefault() *Composer {
	composer, err := New(DefaultCore)
	if err != nil {
		panic(err)
	}
	return composer
}

// StaticSection constructs a named section with fixed text.
func StaticSection(id string, kind SectionKind, text string) Section {
	return Section{ID: id, Kind: kind, Text: text}
}

// Build resolves one immutable section snapshot.
func (c *Composer) Build(ctx context.Context, request Request) (Result, error) {
	return c.BuildWith(ctx, request)
}

// BuildWith resolves one immutable section snapshot with request-scoped
// sections overlaid by identity. It joins non-empty text with exactly two
// newline characters. This allows a session builder to add discovered context
// without mutating the shared section registry.
func (c *Composer) BuildWith(ctx context.Context, _ Request, requestSections ...Section) (Result, error) {
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	core := c.core
	sectionsByKey := make(map[sectionKey]Section, len(requestSections))
	for _, section := range requestSections {
		normalized, err := normalizeSection(section)
		if err != nil {
			return Result{}, err
		}
		sectionsByKey[sectionKey{kind: normalized.Kind, id: normalized.ID}] = normalized
	}

	sections := make([]Section, 0, len(sectionsByKey))
	for _, section := range sectionsByKey {
		sections = append(sections, section)
	}
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].Kind != sections[j].Kind {
			return sections[i].Kind < sections[j].Kind
		}
		return sections[i].ID < sections[j].ID
	})

	result := Result{
		Sources: []Source{{SectionID: "kit.core", ID: "kit.core", Kind: SectionCore}},
	}
	parts := []string{core}
	for _, section := range sections {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		result.Diagnostics = append(result.Diagnostics, section.Diagnostics...)
		if section.Text == "" {
			continue
		}
		parts = append(parts, section.Text)
		if len(section.Sources) == 0 {
			result.Sources = append(result.Sources, Source{
				SectionID: section.ID,
				ID:        section.ID,
				Kind:      section.Kind,
			})
		} else {
			result.Sources = append(result.Sources, section.Sources...)
		}
	}
	result.Prompt = strings.Join(parts, "\n\n")
	return result, nil
}

func (c *Composer) validate() error {
	if c == nil {
		return errors.New("system prompt composer is nil")
	}
	if c.core == "" {
		return errors.New("system prompt composer is not initialized")
	}
	return nil
}

func normalizeSection(section Section) (Section, error) {
	if section.ID == "" || section.ID != strings.TrimSpace(section.ID) {
		return Section{}, errors.New("system prompt section id must be non-empty without surrounding whitespace")
	}
	if !validContributionKind(section.Kind) {
		return Section{}, fmt.Errorf("invalid system prompt section kind %d", section.Kind)
	}

	normalized := cloneSection(section)
	normalized.Text = strings.TrimSpace(normalized.Text)
	seenSources := make(map[string]struct{}, len(normalized.Sources))
	for index := range normalized.Sources {
		source := &normalized.Sources[index]
		if source.ID == "" || source.ID != strings.TrimSpace(source.ID) {
			return Section{}, fmt.Errorf("system prompt section %q has a source without a valid id", section.ID)
		}
		if _, exists := seenSources[source.ID]; exists {
			return Section{}, fmt.Errorf("system prompt section %q has duplicate source id %q", section.ID, source.ID)
		}
		seenSources[source.ID] = struct{}{}
		source.SectionID = normalized.ID
		source.Kind = normalized.Kind
	}
	for index := range normalized.Diagnostics {
		diagnostic := &normalized.Diagnostics[index]
		if diagnostic.Severity != DiagnosticInfo && diagnostic.Severity != DiagnosticWarning {
			return Section{}, fmt.Errorf("system prompt section %q has invalid diagnostic severity %q", section.ID, diagnostic.Severity)
		}
		if diagnostic.Code == "" || diagnostic.Code != strings.TrimSpace(diagnostic.Code) {
			return Section{}, fmt.Errorf("system prompt section %q has a diagnostic without a valid code", section.ID)
		}
		diagnostic.Message = strings.TrimSpace(diagnostic.Message)
		if diagnostic.Message == "" {
			return Section{}, fmt.Errorf("system prompt section %q has a diagnostic without a message", section.ID)
		}
		if diagnostic.Source.ID == "" {
			diagnostic.Source.ID = normalized.ID
		}
		if diagnostic.Source.ID != strings.TrimSpace(diagnostic.Source.ID) {
			return Section{}, fmt.Errorf("system prompt section %q has a diagnostic source without a valid id", section.ID)
		}
		diagnostic.Source.SectionID = normalized.ID
		diagnostic.Source.Kind = normalized.Kind
	}
	return normalized, nil
}

func cloneSection(section Section) Section {
	section.Sources = append([]Source(nil), section.Sources...)
	section.Diagnostics = append([]Diagnostic(nil), section.Diagnostics...)
	return section
}

func validContributionKind(kind SectionKind) bool {
	return kind >= SectionFeature && kind <= SectionContext
}
