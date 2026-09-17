// Package boundeddiff is a research prototype, not Kit production code.
// It combines patience anchors with a linear-space Hirschberg LCS fallback.
package boundeddiff

import (
	"context"
	"errors"
	"math"
)

var ErrTooComplex = errors.New("diff work budget exhausted")

type Kind uint8

const (
	Equal Kind = iota
	Delete
	Insert
)

type Edit struct {
	Kind Kind
	Text string
}
type Limits struct {
	MaxWork        int64
	MaxMemoryBytes int64
	MaxEdits       int
}
type state struct {
	ctx   context.Context
	lim   Limits
	work  int64
	edits []Edit
}

func Diff(ctx context.Context, old, new []string, lim Limits) ([]Edit, error) {
	if lim.MaxWork <= 0 || lim.MaxMemoryBytes <= 0 || lim.MaxEdits <= 0 {
		return nil, ErrTooComplex
	}
	// Conservative scratch model: occurrence maps plus their keys/buckets,
	// anchor/LIS slices, output capacity, recursion, and two LCS rows. Inputs are
	// caller-owned. This is intentionally higher than measured Go allocations.
	memory := int64(4096)
	if !addEstimate(&memory, len(old)+len(new), 320) ||
		!addEstimate(&memory, lim.MaxEdits, 64) ||
		!addEstimate(&memory, min(len(old), len(new))+1, 16) ||
		memory > lim.MaxMemoryBytes {
		return nil, ErrTooComplex
	}
	s := &state{ctx: ctx, lim: lim}
	if err := s.segment(old, new); err != nil {
		return nil, err
	}
	return s.edits, nil
}

func addEstimate(total *int64, count int, unit int64) bool {
	if count < 0 || unit <= 0 || int64(count) > (math.MaxInt64-*total)/unit {
		return false
	}
	*total += int64(count) * unit
	return true
}

func (s *state) spend(n int64) error {
	s.work += n
	if s.work > s.lim.MaxWork {
		return ErrTooComplex
	}
	if s.work&1023 == 0 {
		return s.ctx.Err()
	}
	return nil
}
func (s *state) emit(k Kind, text string) error {
	if len(s.edits) >= s.lim.MaxEdits {
		return ErrTooComplex
	}
	s.edits = append(s.edits, Edit{k, text})
	return nil
}

// segment uses unique-line patience anchors, then exact linear-space LCS in gaps.
func (s *state) segment(a, b []string) error {
	anchors, err := s.patienceAnchors(a, b)
	if err != nil {
		return err
	}
	if len(anchors) == 0 {
		return s.hirschberg(a, b)
	}
	ai, bi := 0, 0
	for _, p := range anchors {
		if err := s.hirschberg(a[ai:p[0]], b[bi:p[1]]); err != nil {
			return err
		}
		if err := s.emit(Equal, a[p[0]]); err != nil {
			return err
		}
		ai, bi = p[0]+1, p[1]+1
	}
	return s.hirschberg(a[ai:], b[bi:])
}

func (s *state) patienceAnchors(a, b []string) ([][2]int, error) {
	type occurrence struct{ count, pos int }
	am, bm := map[string]occurrence{}, map[string]occurrence{}
	for i, x := range a {
		if err := s.spend(1); err != nil {
			return nil, err
		}
		o := am[x]
		o.count++
		o.pos = i
		am[x] = o
	}
	for i, x := range b {
		if err := s.spend(1); err != nil {
			return nil, err
		}
		o := bm[x]
		o.count++
		o.pos = i
		bm[x] = o
	}
	pairs := make([][2]int, 0)
	for i, x := range a {
		if err := s.spend(1); err != nil {
			return nil, err
		}
		ao, bo := am[x], bm[x]
		if ao.count == 1 && bo.count == 1 {
			pairs = append(pairs, [2]int{i, bo.pos})
		}
	}
	// LIS by second coordinate, deterministic lower-bound replacement.
	tails, prev := make([]int, 0, len(pairs)), make([]int, len(pairs))
	for i, p := range pairs {
		if err := s.spend(1); err != nil {
			return nil, err
		}
		lo, hi := 0, len(tails)
		for lo < hi {
			m := (lo + hi) / 2
			if pairs[tails[m]][1] < p[1] {
				lo = m + 1
			} else {
				hi = m
			}
		}
		prev[i] = -1
		if lo > 0 {
			prev[i] = tails[lo-1]
		}
		if lo == len(tails) {
			tails = append(tails, i)
		} else {
			tails[lo] = i
		}
	}
	if len(tails) == 0 {
		return nil, nil
	}
	out := make([][2]int, len(tails))
	k := tails[len(tails)-1]
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = pairs[k]
		k = prev[k]
	}
	return out, nil
}

func (s *state) hirschberg(a, b []string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if len(a) == 0 {
		for _, x := range b {
			if err := s.emit(Insert, x); err != nil {
				return err
			}
		}
		return nil
	}
	if len(b) == 0 {
		for _, x := range a {
			if err := s.emit(Delete, x); err != nil {
				return err
			}
		}
		return nil
	}
	if len(a) == 1 {
		at := -1
		for j, x := range b {
			if err := s.spend(1); err != nil {
				return err
			}
			if at < 0 && a[0] == x {
				at = j
			}
		}
		if at < 0 {
			if err := s.emit(Delete, a[0]); err != nil {
				return err
			}
			for _, x := range b {
				if err := s.emit(Insert, x); err != nil {
					return err
				}
			}
			return nil
		}
		for _, x := range b[:at] {
			if err := s.emit(Insert, x); err != nil {
				return err
			}
		}
		if err := s.emit(Equal, a[0]); err != nil {
			return err
		}
		for _, x := range b[at+1:] {
			if err := s.emit(Insert, x); err != nil {
				return err
			}
		}
		return nil
	}
	mid := len(a) / 2
	left, err := s.row(a[:mid], b, false)
	if err != nil {
		return err
	}
	right, err := s.row(a[mid:], b, true)
	if err != nil {
		return err
	}
	cut, best := 0, -1
	for j := 0; j <= len(b); j++ {
		score := left[j] + right[len(b)-j]
		if score > best {
			best = score
			cut = j
		}
	}
	if err := s.hirschberg(a[:mid], b[:cut]); err != nil {
		return err
	}
	return s.hirschberg(a[mid:], b[cut:])
}
func (s *state) row(a, b []string, reverse bool) ([]int, error) {
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for i := 0; i < len(a); i++ {
		for j := 0; j < len(b); j++ {
			if err := s.spend(1); err != nil {
				return nil, err
			}
			ai, bj := i, j
			if reverse {
				ai = len(a) - 1 - i
				bj = len(b) - 1 - j
			}
			if a[ai] == b[bj] {
				cur[j+1] = prev[j] + 1
			} else if cur[j] >= prev[j+1] {
				cur[j+1] = cur[j]
			} else {
				cur[j+1] = prev[j+1]
			}
		}
		prev, cur = cur, prev
		clear(cur)
	}
	return prev, nil
}
