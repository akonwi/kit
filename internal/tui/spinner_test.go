package tui

import (
	"testing"
	"time"
)

func TestSpinnerAdvancesAtStandardCadence(t *testing.T) {
	t.Parallel()

	start := time.Unix(1, 0)
	index, tick, changed := advanceSpinner(0, time.Time{}, start)
	if index != 0 || tick != start || changed {
		t.Fatalf("initial spinner tick = %d, %v, %v", index, tick, changed)
	}
	index, tick, changed = advanceSpinner(index, tick, start.Add(79*time.Millisecond))
	if index != 0 || changed {
		t.Fatalf("early spinner tick = %d, %v", index, changed)
	}
	index, tick, changed = advanceSpinner(index, tick, start.Add(80*time.Millisecond))
	if index != 1 || !changed || tick != start.Add(80*time.Millisecond) {
		t.Fatalf("spinner tick = %d, %v, %v", index, tick, changed)
	}
	index, _, changed = advanceSpinner(index, tick, start.Add(400*time.Millisecond))
	if index != 5 || !changed {
		t.Fatalf("coalesced spinner tick = %d, %v", index, changed)
	}
}
