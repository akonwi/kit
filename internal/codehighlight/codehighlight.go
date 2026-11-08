// Package codehighlight serves the semantic highlighting of internal/highlight
// to protocol clients as contract Highlight records (ADR 0062).
//
// It adds no queue, worker pool, or cache: every request goes through the
// highlighter it is given, normally the process's shared highlight.Default
// scheduler, whose bounded workers, queue, cache, and timeout apply. Each
// call shares one deadline across all of its texts, so a caller never waits
// longer than that deadline for colors, and any text not colored in time,
// in an unsupported language, over the highlighter's bounds, or whose result
// fails validation, has no Highlight.
package codehighlight

import (
	"context"
	"sync/atomic"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/highlight"
)

// Service projects highlighter results onto the session contract.
type Service struct {
	highlighter highlight.Highlighter
	timeout     time.Duration
	parallelism int
}

// New returns a service over highlighter. Each call waits at most timeout,
// and highlights at most parallelism texts at once.
func New(highlighter highlight.Highlighter, timeout time.Duration, parallelism int) *Service {
	return &Service{highlighter: highlighter, timeout: timeout, parallelism: max(1, parallelism)}
}

// Default serves the process's shared highlighter. Its parallelism matches
// the scheduler's workers, so one request cannot fill the scheduler's queue.
var Default = New(highlight.Default, highlight.DefaultHighlightTimeout, highlight.DefaultWorkers)

// Text identifies one text to highlight by language label or path.
type Text struct {
	Language string
	Path     string
	Source   string
}

// Highlight returns the highlighting of text, or nil.
func (s *Service) Highlight(ctx context.Context, text Text) *protocol.Highlight {
	return s.Highlights(ctx, []Text{text})[0]
}

// Highlights returns one highlighting per text, in order, nil for a text it
// could not color. All texts share one deadline, and Highlights returns by
// it even when the highlighter does not honor cancellation promptly.
func (s *Service) Highlights(ctx context.Context, texts []Text) []*protocol.Highlight {
	results := make([]*protocol.Highlight, len(texts))
	if len(texts) == 0 {
		return results
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	// Cancelling on return stops the workers: each finishes at most the text
	// in hand, whose result it sends into the buffer and nobody reads.
	defer cancel()
	type indexed struct {
		index     int
		highlight *protocol.Highlight
	}
	finished := make(chan indexed, len(texts))
	var next atomic.Int64
	for range min(s.parallelism, len(texts)) {
		go func() {
			for {
				i := int(next.Add(1) - 1)
				if i >= len(texts) || ctx.Err() != nil {
					return
				}
				finished <- indexed{index: i, highlight: s.highlight(ctx, texts[i])}
			}
		}()
	}
	for range texts {
		select {
		case result := <-finished:
			results[result.index] = result.highlight
		case <-ctx.Done():
			return results
		}
	}
	return results
}

// Code highlights a batch of code blocks.
func (s *Service) Code(ctx context.Context, input protocol.HighlightCodeInput) protocol.HighlightCodeResult {
	texts := make([]Text, len(input.Blocks))
	for i, block := range input.Blocks {
		texts[i] = Text{Language: block.Language, Source: block.Source}
	}
	highlights := s.Highlights(ctx, texts)
	result := protocol.HighlightCodeResult{Blocks: make([]protocol.HighlightedCode, len(highlights))}
	for i, highlight := range highlights {
		result.Blocks[i].Highlight = highlight
	}
	return result
}

func (s *Service) highlight(ctx context.Context, text Text) *protocol.Highlight {
	// Text the highlighter cannot color never takes a scheduler slot.
	if !highlight.Supported(text.Language, text.Path) || len(text.Source) > highlight.MaxSourceBytes {
		return nil
	}
	result := s.highlighter.Highlight(ctx, highlight.Request{Language: text.Language, Path: text.Path, Source: text.Source})
	if ctx.Err() != nil {
		return nil
	}
	lines, ok := highlight.Lines(text.Source, result)
	if !ok {
		return nil
	}
	projected := project(result.Language, lines)
	if projected.ValidateText(text.Source) != nil {
		return nil
	}
	return &projected
}

// project encodes mapped lines as a Highlight, numbering roles in order of
// first use.
func project(language string, lines [][]highlight.LineSpan) protocol.Highlight {
	projected := protocol.Highlight{Language: language, Lines: make([][]int, len(lines)), Roles: []string{}}
	roles := map[highlight.Role]int{}
	for i, spans := range lines {
		triples := make([]int, 0, 3*len(spans))
		for _, span := range spans {
			role, ok := roles[span.Role]
			if !ok {
				role = len(projected.Roles)
				roles[span.Role] = role
				projected.Roles = append(projected.Roles, string(span.Role))
			}
			triples = append(triples, span.Start, span.Length, role)
		}
		projected.Lines[i] = triples
	}
	return projected
}

// SelectLines returns the highlighting of the given lines of h, by
// zero-based index, in the order given, with only the roles those lines use,
// numbered in order of first use. It returns nil when h is nil or an index is
// out of range.
func SelectLines(h *protocol.Highlight, indexes []int) *protocol.Highlight {
	if h == nil {
		return nil
	}
	selected := protocol.Highlight{Language: h.Language, Lines: make([][]int, len(indexes)), Roles: []string{}}
	renumbered := map[int]int{}
	for i, index := range indexes {
		if index < 0 || index >= len(h.Lines) {
			return nil
		}
		triples := append([]int(nil), h.Lines[index]...)
		if triples == nil {
			triples = []int{}
		}
		for j := 2; j < len(triples); j += 3 {
			role, ok := renumbered[triples[j]]
			if !ok {
				role = len(selected.Roles)
				renumbered[triples[j]] = role
				selected.Roles = append(selected.Roles, h.Roles[triples[j]])
			}
			triples[j] = role
		}
		selected.Lines[i] = triples
	}
	return &selected
}
