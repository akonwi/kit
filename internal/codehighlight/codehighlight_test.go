package codehighlight

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/highlight"
)

// recordingHighlighter counts calls and peak concurrency, and blocks each
// call until release is closed or the call's context ends.
type recordingHighlighter struct {
	calls   atomic.Int64
	active  atomic.Int64
	peak    atomic.Int64
	release chan struct{}
	inner   highlight.Highlighter
}

func (h *recordingHighlighter) Highlight(ctx context.Context, request highlight.Request) highlight.Result {
	h.calls.Add(1)
	active := h.active.Add(1)
	defer h.active.Add(-1)
	for {
		peak := h.peak.Load()
		if active <= peak || h.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	if h.release != nil {
		select {
		case <-h.release:
		case <-ctx.Done():
			return highlight.Plain(request, ctx.Err())
		}
	}
	if h.inner != nil {
		return h.inner.Highlight(ctx, request)
	}
	return highlight.Plain(request, nil)
}

func newScheduler(t *testing.T, workers, queue int) *highlight.Scheduler {
	t.Helper()
	scheduler := highlight.NewScheduler(workers, queue, 1<<20)
	t.Cleanup(scheduler.Close)
	return scheduler
}

func TestHighlightProjectsRolesAndUTF16Ranges(t *testing.T) {
	t.Parallel()
	service := New(newScheduler(t, 1, 1), time.Second, 1)
	source := "package main\r\n\r\n// 😀 é\tok\r\nfunc main() {}\r\n"
	got := service.Highlight(t.Context(), Text{Path: "main.go", Source: source})
	want := &protocol.Highlight{
		Language: "go",
		Lines:    [][]int{{0, 7, 0}, {}, {0, 10, 1}, {0, 4, 0, 5, 4, 2}},
		Roles:    []string{"keyword", "comment", "function"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Highlight() = %#v, want %#v", got, want)
	}
}

func TestTextTheHighlighterCannotColorNeverReachesIt(t *testing.T) {
	t.Parallel()
	recorder := &recordingHighlighter{}
	service := New(recorder, time.Second, 2)
	for name, text := range map[string]Text{
		"no language":          {Source: "x"},
		"unsupported language": {Language: "cobol", Source: "x"},
		"unsupported path":     {Path: "notes.txt", Source: "x"},
		"over the bound":       {Language: "go", Source: string(make([]byte, highlight.MaxSourceBytes+1))},
	} {
		if got := service.Highlight(t.Context(), text); got != nil {
			t.Errorf("%s: Highlight() = %#v, want nil", name, got)
		}
	}
	if calls := recorder.calls.Load(); calls != 0 {
		t.Fatalf("highlighter calls = %d, want 0", calls)
	}
}

func TestPlainResultsHaveNoHighlight(t *testing.T) {
	t.Parallel()
	// The recorder returns plain text for a supported language, as the
	// scheduler does on saturation, failure, or timeout.
	service := New(&recordingHighlighter{}, time.Second, 1)
	if got := service.Highlight(t.Context(), Text{Language: "go", Source: "package main\n"}); got != nil {
		t.Fatalf("Highlight() = %#v, want nil", got)
	}
}

func TestHighlightsShareOneDeadline(t *testing.T) {
	t.Parallel()
	recorder := &recordingHighlighter{release: make(chan struct{})}
	const timeout = 50 * time.Millisecond
	service := New(recorder, timeout, 2)
	texts := make([]Text, 8)
	for i := range texts {
		texts[i] = Text{Language: "go", Source: "package main\n"}
	}
	started := time.Now()
	results := service.Highlights(t.Context(), texts)
	elapsed := time.Since(started)
	if elapsed > 10*timeout {
		t.Fatalf("Highlights() took %v with a %v deadline", elapsed, timeout)
	}
	for i, result := range results {
		if result != nil {
			t.Fatalf("result %d = %#v, want nil after the deadline", i, result)
		}
	}
	if calls := recorder.calls.Load(); calls != 2 {
		t.Fatalf("highlighter calls = %d, want the 2 in flight at the deadline", calls)
	}
}

// ignoringHighlighter ignores cancellation and answers only once released.
type ignoringHighlighter struct {
	release chan struct{}
	calls   atomic.Int64
}

func (h *ignoringHighlighter) Highlight(_ context.Context, request highlight.Request) highlight.Result {
	h.calls.Add(1)
	<-h.release
	return highlight.Plain(request, nil)
}

func TestHighlightsReturnByTheDeadlineWhenTheHighlighterIgnoresIt(t *testing.T) {
	t.Parallel()
	ignoring := &ignoringHighlighter{release: make(chan struct{})}
	t.Cleanup(func() { close(ignoring.release) })
	const timeout = 50 * time.Millisecond
	service := New(ignoring, timeout, 2)
	started := time.Now()
	results := service.Highlights(t.Context(), []Text{{Language: "go", Source: "package a\n"}, {Language: "go", Source: "package b\n"}, {Language: "go", Source: "package c\n"}})
	if elapsed := time.Since(started); elapsed > 10*timeout {
		t.Fatalf("Highlights() took %v with a %v deadline", elapsed, timeout)
	}
	if !reflect.DeepEqual(results, make([]*protocol.Highlight, 3)) {
		t.Fatalf("Highlights() = %v, want every text plain", results)
	}
	if calls := ignoring.calls.Load(); calls != 2 {
		t.Fatalf("highlighter calls = %d, want the 2 in flight at the deadline", calls)
	}
}

func TestCodeKeepsBlockOrderAndBoundsParallelism(t *testing.T) {
	t.Parallel()
	recorder := &recordingHighlighter{inner: newScheduler(t, 2, 8)}
	service := New(recorder, 5*time.Second, 2)
	input := protocol.HighlightCodeInput{Blocks: []protocol.HighlightCodeBlock{
		{Language: "go", Source: "package a\n"},
		{Language: "", Source: "plain"},
		{Language: "py", Source: "def f():\n    return 1\n"},
		{Language: "cobol", Source: "x"},
		{Language: "ts", Source: "const x = 1"},
		{Language: "golang", Source: "func f() {}"},
	}}
	result := service.Code(t.Context(), input)
	if err := result.Validate(input); err != nil {
		t.Fatalf("Code() result is invalid: %v", err)
	}
	var languages []string
	for _, block := range result.Blocks {
		if block.Highlight == nil {
			languages = append(languages, "")
		} else {
			languages = append(languages, block.Highlight.Language)
		}
	}
	if want := []string{"go", "", "python", "", "typescript", "go"}; !reflect.DeepEqual(languages, want) {
		t.Fatalf("highlighted languages = %q, want %q", languages, want)
	}
	if peak := recorder.peak.Load(); peak > 2 {
		t.Fatalf("peak concurrent highlights = %d, want at most 2", peak)
	}
}

func TestSelectLinesPicksPageLines(t *testing.T) {
	t.Parallel()
	whole := &protocol.Highlight{Language: "go", Lines: [][]int{{0, 1, 0}, {}, {0, 2, 1}}, Roles: []string{"keyword", "string"}}
	got := SelectLines(whole, []int{2, 0})
	want := &protocol.Highlight{Language: "go", Lines: [][]int{{0, 2, 0}, {0, 1, 1}}, Roles: []string{"string", "keyword"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SelectLines() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(whole.Lines[2], []int{0, 2, 1}) {
		t.Fatalf("SelectLines() changed its source: %v", whole.Lines)
	}
	if got := SelectLines(whole, []int{1}); !reflect.DeepEqual(got, &protocol.Highlight{Language: "go", Lines: [][]int{{}}, Roles: []string{}}) {
		t.Fatalf("SelectLines() of an unstyled line = %#v", got)
	}
	if SelectLines(whole, []int{3}) != nil || SelectLines(nil, []int{0}) != nil {
		t.Fatal("SelectLines() selected a line that does not exist")
	}
}

func TestConcurrentCallersShareTheSchedulerSafely(t *testing.T) {
	t.Parallel()
	scheduler := newScheduler(t, 2, 8)
	service := New(scheduler, 2*time.Second, 2)
	var wg sync.WaitGroup
	for caller := range 32 {
		wg.Go(func() {
			source := "package main\n\nfunc f() int { return " + string(rune('0'+caller%10)) + " }\n"
			for range 5 {
				if got := service.Highlight(t.Context(), Text{Language: "go", Source: source}); got != nil {
					if err := got.ValidateText(source); err != nil {
						t.Errorf("caller %d got an invalid highlight: %v", caller, err)
					}
				}
			}
		})
	}
	wg.Wait()
}
