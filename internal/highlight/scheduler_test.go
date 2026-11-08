package highlight

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Remote clients share the server's scheduler. Once its queue is full, a
// request returns plain at once rather than waiting for a worker.
func TestSaturatedSchedulerReturnsPlainAtOnce(t *testing.T) {
	t.Parallel()
	// No workers run, so the one queued job holds the queue full.
	scheduler := &Scheduler{jobs: make(chan job, 1), cache: newResultCache(0)}
	scheduler.jobs <- job{ctx: context.Background(), request: Request{Language: "go"}, response: make(chan Result, 1)}
	request := Request{Language: "go", Source: "package main\n"}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			started := time.Now()
			result := scheduler.Highlight(context.Background(), request)
			if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
				t.Errorf("saturated Highlight() took %v", elapsed)
			}
			if !result.Fallback || !errors.Is(result.Err, errQueueFull) || result.Source != request.Source {
				t.Errorf("saturated Highlight() = %+v, want a plain queue-full fallback", result)
			}
		})
	}
	wg.Wait()
}

// Many concurrent callers, some cancelling, all get an answer within the
// highlighter's timeout, and closing the scheduler while they run is safe.
func TestSchedulerUnderConcurrentLoadAndClose(t *testing.T) {
	t.Parallel()
	scheduler := NewScheduler(DefaultWorkers, DefaultQueueDepth, 1<<20)
	var wg sync.WaitGroup
	for caller := range 64 {
		wg.Go(func() {
			ctx := context.Background()
			if caller%4 == 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(caller%7)*time.Millisecond)
				defer cancel()
			}
			source := "package main\n\nfunc f() int { return " + string(rune('0'+caller%10)) + " }\n"
			started := time.Now()
			result := scheduler.Highlight(ctx, Request{Language: "go", Source: source})
			if elapsed := time.Since(started); elapsed > DefaultHighlightTimeout+250*time.Millisecond {
				t.Errorf("Highlight() took %v", elapsed)
			}
			if result.Source != Sanitize(source) {
				t.Errorf("Highlight() source = %q", result.Source)
			}
			if _, ok := Lines(source, result); !result.Fallback && !ok {
				t.Errorf("highlighted result does not map to its source")
			}
		})
	}
	time.Sleep(5 * time.Millisecond)
	scheduler.Close()
	wg.Wait()
	if result := scheduler.Highlight(context.Background(), Request{Language: "go", Source: "x"}); !result.Fallback {
		t.Fatal("closed scheduler highlighted")
	}
}
