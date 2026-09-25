package highlight

import (
	"container/heap"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	treesittertoml "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go"
	treesitteryaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"
	treesitterbash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	treesittercss "github.com/tree-sitter/tree-sitter-css/bindings/go"
	treesittergo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	treesitterhtml "github.com/tree-sitter/tree-sitter-html/bindings/go"
	treesitterjavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	treesitterjson "github.com/tree-sitter/tree-sitter-json/bindings/go"
	treesitterpython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	treesitterrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	treesittertypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/akonwi/kit/internal/highlight/grammarmarkdown"
	"github.com/akonwi/kit/internal/highlight/grammarmarkdowninline"
	"github.com/akonwi/kit/internal/highlight/grammarsql"
	treesitterard "github.com/akonwi/tree-sitter-ard/bindings/go"
)

const (
	MaxSourceBytes         = 512 << 10
	maxCaptures            = 100_000
	maxMatches             = 100_000
	maxOutputSpans         = 100_000
	maxInjectionDepth      = 4
	maxInjectedBytes       = 1 << 20
	nativeMatchLimit       = 16_384
	maxNativeQueryDuration = 100 * time.Millisecond
)

//go:embed queries/ard.scm
var ardQuery string

//go:embed queries/go.scm
var goQuery string

//go:embed queries/javascript.scm
var javascriptQuery string

//go:embed queries/typescript.scm
var typescriptQuery string

//go:embed queries/tsx.scm
var tsxQuery string

//go:embed queries/python.scm
var pythonQuery string

//go:embed queries/rust.scm
var rustQuery string

//go:embed queries/bash.scm
var bashQuery string

//go:embed queries/json.scm
var jsonQuery string

//go:embed queries/yaml.scm
var yamlQuery string

//go:embed queries/toml.scm
var tomlQuery string

//go:embed queries/markdown.scm
var markdownQuery string

//go:embed queries/markdown-injections.scm
var markdownInjections string

//go:embed queries/markdown_inline.scm
var markdownInlineQuery string

//go:embed queries/markdown_inline-injections.scm
var markdownInlineInjections string

//go:embed queries/html.scm
var htmlQuery string

//go:embed queries/html-injections.scm
var htmlInjections string

//go:embed queries/css.scm
var cssQuery string

//go:embed queries/sql.scm
var sqlQuery string

type grammarSpec struct {
	language   *sitter.Language
	highlights string
	injections string
}

var nativeGrammars = map[string]grammarSpec{
	"ard":             {sitter.NewLanguage(treesitterard.Language()), ardQuery, ""},
	"go":              {sitter.NewLanguage(treesittergo.Language()), goQuery, ""},
	"javascript":      {sitter.NewLanguage(treesitterjavascript.Language()), javascriptQuery, ""},
	"typescript":      {sitter.NewLanguage(treesittertypescript.LanguageTypescript()), typescriptQuery, ""},
	"tsx":             {sitter.NewLanguage(treesittertypescript.LanguageTSX()), tsxQuery, ""},
	"python":          {sitter.NewLanguage(treesitterpython.Language()), pythonQuery, ""},
	"rust":            {sitter.NewLanguage(treesitterrust.Language()), rustQuery, ""},
	"bash":            {sitter.NewLanguage(treesitterbash.Language()), bashQuery, ""},
	"json":            {sitter.NewLanguage(treesitterjson.Language()), jsonQuery, ""},
	"yaml":            {sitter.NewLanguage(treesitteryaml.Language()), yamlQuery, ""},
	"toml":            {sitter.NewLanguage(treesittertoml.Language()), tomlQuery, ""},
	"markdown":        {sitter.NewLanguage(grammarmarkdown.Language()), markdownQuery, markdownInjections},
	"markdown_inline": {sitter.NewLanguage(grammarmarkdowninline.Language()), markdownInlineQuery, markdownInlineInjections},
	"html":            {sitter.NewLanguage(treesitterhtml.Language()), htmlQuery, htmlInjections},
	"css":             {sitter.NewLanguage(treesittercss.Language()), cssQuery, ""},
	"sql":             {sitter.NewLanguage(grammarsql.Language()), sqlQuery, ""},
}

func SupportedLanguages() []string {
	return []string{"ard", "bash", "css", "go", "html", "javascript", "json", "markdown", "python", "rust", "sql", "toml", "tsx", "typescript", "yaml"}
}

type compiledGrammar struct {
	spec       grammarSpec
	highlights *sitter.Query
	injections *sitter.Query
}

type treeSitterEngine struct {
	parser          *sitter.Parser
	cancellation    *uintptr
	cancellationPin runtime.Pinner
	grammars        map[string]*compiledGrammar
}

func (e *treeSitterEngine) close() {
	if e.parser != nil {
		e.parser.SetCancellationFlag(nil)
		e.parser.Close()
		e.parser = nil
	}
	if e.cancellation != nil {
		e.cancellationPin.Unpin()
		e.cancellation = nil
	}
	for _, grammar := range e.grammars {
		if grammar.highlights != nil {
			grammar.highlights.Close()
		}
		if grammar.injections != nil {
			grammar.injections.Close()
		}
	}
	e.grammars = nil
}

func (e *treeSitterEngine) grammar(name string) (*compiledGrammar, error) {
	if e.parser == nil {
		e.parser = sitter.NewParser()
		e.cancellation = new(uintptr)
		e.cancellationPin.Pin(e.cancellation)
		e.parser.SetCancellationFlag(e.cancellation)
		e.grammars = make(map[string]*compiledGrammar)
	}
	if grammar := e.grammars[name]; grammar != nil {
		return grammar, nil
	}
	spec, ok := nativeGrammars[name]
	if !ok {
		return nil, fmt.Errorf("native tree-sitter grammar unavailable: %s", name)
	}
	highlights, queryErr := sitter.NewQuery(spec.language, spec.highlights)
	if queryErr != nil {
		return nil, fmt.Errorf("compile %s highlights query: %w", name, queryErr)
	}
	disableUnsupportedPredicatePatterns(highlights)
	grammar := &compiledGrammar{spec: spec, highlights: highlights}
	if spec.injections != "" {
		injections, injectionErr := sitter.NewQuery(spec.language, spec.injections)
		if injectionErr != nil {
			highlights.Close()
			return nil, fmt.Errorf("compile %s injections query: %w", name, injectionErr)
		}
		disableUnsupportedPredicatePatterns(injections)
		grammar.injections = injections
	}
	e.grammars[name] = grammar
	return grammar, nil
}

func disableUnsupportedPredicatePatterns(query *sitter.Query) {
	for pattern := range query.PatternCount() {
		if len(query.PropertyPredicates(pattern)) > 0 || len(query.GeneralPredicates(pattern)) > 0 {
			query.DisablePattern(pattern)
		}
	}
}

func (e *treeSitterEngine) highlight(ctx context.Context, request Request) Result {
	ctx, cancel := context.WithTimeout(ctx, DefaultHighlightTimeout)
	defer cancel()
	source := Sanitize(request.Source)
	language := DetectLanguage(request.Language, request.Path)
	fallback := Result{Source: source, Language: language, Fallback: true}
	if len(source) > MaxSourceBytes {
		fallback.Err = fmt.Errorf("source exceeds %d-byte parser bound", MaxSourceBytes)
		return fallback
	}
	if err := ctx.Err(); err != nil {
		fallback.Err = err
		return fallback
	}
	budget := highlightBudget{}
	captures, err := e.highlightLanguage(ctx, language, []byte(source), 0, 0, &budget)
	if err != nil {
		fallback.Err = err
		return fallback
	}
	spans, err := flattenCaptures(ctx, len(source), captures)
	if err != nil {
		fallback.Err = err
		return fallback
	}
	return Result{Source: source, Language: language, Spans: spans}
}

type highlightBudget struct {
	matches       int
	captures      int
	injectedBytes int
}

func (e *treeSitterEngine) highlightLanguage(ctx context.Context, language string, source []byte, baseOffset, depth int, budget *highlightBudget) ([]captureSpan, error) {
	if depth > maxInjectionDepth {
		return nil, errors.New("tree-sitter injection depth exceeded")
	}
	grammar, err := e.grammar(language)
	if err != nil {
		return nil, err
	}
	if err := e.parser.SetLanguage(grammar.spec.language); err != nil {
		return nil, fmt.Errorf("set %s grammar: %w", language, err)
	}
	e.parser.Reset()
	tree := e.parseWithContext(ctx, source)
	if tree == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("native tree-sitter parse stopped")
	}
	defer tree.Close()
	root := tree.RootNode()
	captures, err := collectQueryCaptures(ctx, grammar.highlights, root, source, baseOffset, depth, budget)
	if err != nil {
		return nil, err
	}
	if grammar.injections == nil || depth == maxInjectionDepth {
		return captures, nil
	}
	injections, err := collectInjections(ctx, grammar.injections, root, source, budget)
	if err != nil {
		return nil, err
	}
	for _, injection := range injections {
		if injection.language == language && injection.start == 0 && injection.end == len(source) {
			continue
		}
		budget.injectedBytes += injection.end - injection.start
		if budget.injectedBytes > maxInjectedBytes {
			return nil, errors.New("tree-sitter injection byte budget exceeded")
		}
		injected, injectionErr := e.highlightLanguage(ctx, injection.language, source[injection.start:injection.end], baseOffset+injection.start, depth+1, budget)
		if injectionErr != nil {
			if strings.Contains(injectionErr.Error(), "grammar unavailable") {
				continue
			}
			return nil, injectionErr
		}
		captures = append(captures, injected...)
	}
	return captures, nil
}

func (e *treeSitterEngine) parseWithContext(ctx context.Context, source []byte) *sitter.Tree {
	atomic.StoreUintptr(e.cancellation, 0)
	if ctx.Done() == nil {
		return e.parser.Parse(source, nil)
	}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			atomic.StoreUintptr(e.cancellation, 1)
		case <-stop:
		}
	}()
	tree := e.parser.Parse(source, nil)
	close(stop)
	<-stopped
	return tree
}

func collectQueryCaptures(ctx context.Context, query *sitter.Query, root *sitter.Node, source []byte, baseOffset, depth int, budget *highlightBudget) ([]captureSpan, error) {
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	cursor.SetMatchLimit(nativeMatchLimit)
	queryTimeout := remainingQueryTimeout(ctx)
	cursor.SetTimeoutMicros(uint64(queryTimeout.Microseconds()))
	queryStarted := time.Now()
	matches := cursor.Matches(query, root, source)
	names := query.CaptureNames()
	captures := make([]captureSpan, 0, 128)
	for match := matches.Next(); match != nil; match = matches.Next() {
		budget.matches++
		budget.captures += len(match.Captures)
		if budget.matches > maxMatches || budget.captures > maxCaptures {
			return nil, errors.New("tree-sitter capture budget exceeded")
		}
		for _, capture := range match.Captures {
			if int(capture.Index) >= len(names) {
				continue
			}
			role, known := captureRole(names[capture.Index])
			if !known {
				continue
			}
			start, end := int(capture.Node.StartByte()), int(capture.Node.EndByte())
			if start < end && end <= len(source) {
				captures = append(captures, captureSpan{Span: Span{Start: baseOffset + start, End: baseOffset + end, Role: role}, depth: depth, priority: capturePrecedence(names[capture.Index], role)})
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if time.Since(queryStarted) >= queryTimeout {
		return nil, errors.New("tree-sitter highlight query timed out")
	}
	if cursor.DidExceedMatchLimit() {
		return nil, errors.New("tree-sitter match limit exceeded")
	}
	return captures, nil
}

type injectionRange struct {
	language   string
	start, end int
}

func collectInjections(ctx context.Context, query *sitter.Query, root *sitter.Node, source []byte, budget *highlightBudget) ([]injectionRange, error) {
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	cursor.SetMatchLimit(nativeMatchLimit)
	queryTimeout := remainingQueryTimeout(ctx)
	cursor.SetTimeoutMicros(uint64(queryTimeout.Microseconds()))
	queryStarted := time.Now()
	matches := cursor.Matches(query, root, source)
	names := query.CaptureNames()
	result := make([]injectionRange, 0, 8)
	for match := matches.Next(); match != nil; match = matches.Next() {
		budget.matches++
		budget.captures += len(match.Captures)
		if budget.matches > maxMatches || budget.captures > maxCaptures {
			return nil, errors.New("tree-sitter injection capture budget exceeded")
		}
		language := ""
		for _, property := range query.PropertySettings(match.PatternIndex) {
			if property.Key == "injection.language" && property.Value != nil {
				language = *property.Value
			}
		}
		contents := make([]sitter.Node, 0, 1)
		for _, capture := range match.Captures {
			if int(capture.Index) >= len(names) {
				continue
			}
			switch names[capture.Index] {
			case "injection.language":
				language = string(source[capture.Node.StartByte():capture.Node.EndByte()])
			case "injection.content":
				contents = append(contents, capture.Node)
			}
		}
		language = DetectLanguage(language, "")
		if language == "" {
			continue
		}
		for _, content := range contents {
			start, end := int(content.StartByte()), int(content.EndByte())
			if start < end && end <= len(source) {
				result = append(result, injectionRange{language: language, start: start, end: end})
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if time.Since(queryStarted) >= queryTimeout {
		return nil, errors.New("tree-sitter injection query timed out")
	}
	if cursor.DidExceedMatchLimit() {
		return nil, errors.New("tree-sitter injection match limit exceeded")
	}
	return result, nil
}

func remainingQueryTimeout(ctx context.Context) time.Duration {
	timeout := maxNativeQueryDuration
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	if timeout < time.Microsecond {
		return time.Microsecond
	}
	return timeout
}

type captureSpan struct {
	Span
	depth    int
	priority int
}

type captureHeap struct {
	captures []captureSpan
	indexes  []int
}

func (h captureHeap) Len() int { return len(h.indexes) }
func (h captureHeap) Less(left, right int) bool {
	a, b := h.captures[h.indexes[left]], h.captures[h.indexes[right]]
	if a.depth != b.depth {
		return a.depth > b.depth
	}
	if a.End-a.Start != b.End-b.Start {
		return a.End-a.Start < b.End-b.Start
	}
	if a.priority != b.priority {
		return a.priority > b.priority
	}
	return h.indexes[left] > h.indexes[right]
}
func (h captureHeap) Swap(left, right int) {
	h.indexes[left], h.indexes[right] = h.indexes[right], h.indexes[left]
}
func (h *captureHeap) Push(value any) { h.indexes = append(h.indexes, value.(int)) }
func (h *captureHeap) Pop() any {
	last := len(h.indexes) - 1
	value := h.indexes[last]
	h.indexes = h.indexes[:last]
	return value
}

func flattenCaptures(ctx context.Context, sourceBytes int, captures []captureSpan) ([]Span, error) {
	if len(captures) == 0 {
		return nil, nil
	}
	starts := make([]int, len(captures))
	points := make([]int, 0, len(captures)*2)
	for index, capture := range captures {
		starts[index] = index
		points = append(points, capture.Start, capture.End)
	}
	sort.Slice(starts, func(left, right int) bool {
		a, b := captures[starts[left]], captures[starts[right]]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.End < b.End
	})
	sort.Ints(points)
	active := &captureHeap{captures: captures, indexes: make([]int, 0, 32)}
	heap.Init(active)
	spans := make([]Span, 0, min(len(points), maxOutputSpans))
	nextStart := 0
	for pointIndex := 0; pointIndex+1 < len(points); pointIndex++ {
		if pointIndex&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		start, end := points[pointIndex], points[pointIndex+1]
		if start == end || start < 0 || end > sourceBytes {
			continue
		}
		for nextStart < len(starts) && captures[starts[nextStart]].Start <= start {
			heap.Push(active, starts[nextStart])
			nextStart++
		}
		for active.Len() > 0 && captures[active.indexes[0]].End <= start {
			heap.Pop(active)
		}
		if active.Len() == 0 {
			continue
		}
		role := captures[active.indexes[0]].Role
		if len(spans) > 0 && spans[len(spans)-1].End == start && spans[len(spans)-1].Role == role {
			spans[len(spans)-1].End = end
		} else {
			if len(spans) >= maxOutputSpans {
				return nil, errors.New("semantic span budget exceeded")
			}
			spans = append(spans, Span{Start: start, End: end, Role: role})
		}
	}
	return spans, nil
}

func capturePrecedence(name string, role Role) int {
	rank := 50
	switch role {
	case Escape:
		rank = 100
	case Builtin:
		rank = 95
	case TagAttribute:
		rank = 94
	case TagDelimiter:
		rank = 93
	case Tag:
		rank = 92
	case Function:
		rank = 90
	case Member:
		rank = 85
	case Type:
		rank = 80
	case Attribute:
		rank = 75
	case KeywordType:
		rank = 72
	case Keyword:
		rank = 70
	case Number:
		rank = 65
	case String:
		rank = 60
	case Comment:
		rank = 55
	case Variable:
		rank = 10
	}
	return rank*100 + strings.Count(name, ".")*10 + len(name)
}

func captureRole(name string) (Role, bool) {
	name = strings.ToLower(name)
	switch {
	case strings.HasPrefix(name, "text.title"), strings.HasPrefix(name, "markup.heading"):
		return Heading, true
	case strings.HasPrefix(name, "text.strong"), strings.HasPrefix(name, "markup.strong"):
		return Bold, true
	case strings.HasPrefix(name, "text.emphasis"), strings.HasPrefix(name, "markup.italic"):
		return Italic, true
	case strings.HasPrefix(name, "text.uri"), strings.HasPrefix(name, "text.reference"), strings.HasPrefix(name, "markup.link"):
		return Link, true
	case strings.HasPrefix(name, "text.literal"), strings.HasPrefix(name, "markup.raw.block"):
		return CodeBlock, true
	case strings.HasPrefix(name, "markup.raw"):
		return CodeInline, true
	case strings.HasPrefix(name, "markup.list"):
		return List, true
	case strings.HasPrefix(name, "markup.quote"):
		return Quote, true
	case strings.HasPrefix(name, "markup.strikethrough"):
		return Strikethrough, true
	case strings.HasPrefix(name, "comment"):
		return Comment, true
	case strings.Contains(name, "escape"):
		return Escape, true
	case strings.HasPrefix(name, "string"):
		return String, true
	case name == "boolean":
		return Builtin, true
	case strings.HasPrefix(name, "number"), strings.HasPrefix(name, "float"):
		return Number, true
	case strings.HasPrefix(name, "keyword.type"), name == "storageclass":
		return KeywordType, true
	case strings.HasPrefix(name, "keyword"):
		return Keyword, true
	case strings.HasPrefix(name, "function.builtin"), strings.HasPrefix(name, "variable.builtin"), strings.HasPrefix(name, "constant.builtin"):
		return Builtin, true
	case strings.HasPrefix(name, "function") || strings.HasPrefix(name, "method"):
		return Function, true
	case strings.HasPrefix(name, "operator"):
		return Operator, true
	case strings.HasPrefix(name, "variable"), strings.HasPrefix(name, "constant"):
		return Variable, true
	case strings.HasPrefix(name, "property"), strings.HasPrefix(name, "field"):
		return Member, true
	case strings.HasPrefix(name, "type") || strings.HasPrefix(name, "constructor"):
		return Type, true
	case strings.HasPrefix(name, "punctuation.bracket"), strings.HasPrefix(name, "punctuation.delimiter"):
		return Punctuation, true
	case strings.HasPrefix(name, "tag.attribute"):
		return TagAttribute, true
	case strings.HasPrefix(name, "tag.delimiter"):
		return TagDelimiter, true
	case strings.HasPrefix(name, "tag"):
		return Tag, true
	case strings.HasPrefix(name, "attribute"):
		return Attribute, true
	case strings.HasPrefix(name, "label"):
		return Label, true
	default:
		return Text, false
	}
}

var (
	errQueueFull       = errors.New("syntax highlighting queue is full")
	errSchedulerClosed = errors.New("syntax highlighting scheduler is closed")
)
