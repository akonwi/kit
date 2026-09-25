// Package workingdiff owns bounded, server-authoritative working-tree observations.
package workingdiff

import (
	"context"
	"errors"
	"sort"

	"github.com/akonwi/kit/internal/protocol"
)

var errTooComplex = errors.New("diff too complex")

type editKind uint8

const (
	equal editKind = iota
	deletion
	addition
)

type textLine struct {
	text string
	lf   bool
}
type edit struct {
	kind editKind
	line textLine
}
type differ struct {
	ctx     context.Context
	work    int64
	edits   []edit
	scratch []int
}

const (
	maxComparisons int64 = 4_000_000
	maxEdits             = 20_000
	maxHunks             = 2_000
)

func splitLines(b []byte) ([]textLine, string) {
	if len(b) == 0 {
		return nil, ""
	}
	out := make([]textLine, 0, 128)
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i-start > protocol.MaxDiffLineBytes {
				return nil, "line_bytes"
			}
			out = append(out, textLine{string(b[start:i]), true})
			start = i + 1
			if len(out) > protocol.MaxDiffFileLines {
				return nil, "line_count"
			}
		}
	}
	if start < len(b) {
		if len(b)-start > protocol.MaxDiffLineBytes {
			return nil, "line_bytes"
		}
		out = append(out, textLine{string(b[start:]), false})
	}
	if len(out) > protocol.MaxDiffFileLines {
		return nil, "line_count"
	}
	return out, ""
}
func semanticHunks(ctx context.Context, a, b []textLine) ([]protocol.DiffHunk, error) {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	if prefix == len(a) && prefix == len(b) {
		return []protocol.DiffHunk{}, nil
	}
	oldStart := max(0, prefix-3)
	newStart := max(0, prefix-3)
	oldEnd := min(len(a), len(a)-suffix+3)
	newEnd := min(len(b), len(b)-suffix+3)
	edits, err := semanticDiff(ctx, a[oldStart:oldEnd], b[newStart:newEnd])
	if err != nil {
		return nil, err
	}
	hunks, err := hunksFromEdits(edits)
	if err != nil {
		return nil, err
	}
	for hunkIndex := range hunks {
		hunk := &hunks[hunkIndex]
		if hunk.OldStart > 0 {
			hunk.OldStart += oldStart
		}
		if hunk.NewStart > 0 {
			hunk.NewStart += newStart
		}
		for lineIndex := range hunk.Lines {
			line := &hunk.Lines[lineIndex]
			if line.OldLine != nil {
				value := *line.OldLine + oldStart
				line.OldLine = &value
			}
			if line.NewLine != nil {
				value := *line.NewLine + newStart
				line.NewLine = &value
			}
		}
	}
	return hunks, nil
}

func semanticDiff(ctx context.Context, a, b []textLine) ([]edit, error) {
	if len(a)+len(b) > maxEdits {
		return nil, errTooComplex
	}
	m := max(len(a), len(b)) + 1 // four rows and occurrence records remain far below 16 MiB at accepted line limits.
	d := &differ{ctx: ctx, edits: make([]edit, 0, min(maxEdits, len(a)+len(b))), scratch: make([]int, 4*m)}
	if err := d.segment(a, b); err != nil {
		return nil, err
	}
	return d.edits, nil
}
func (d *differ) spendN(n int64) error {
	if n < 0 || d.work > maxComparisons-n {
		return errTooComplex
	}
	d.work += n
	return d.ctx.Err()
}
func (d *differ) spend() error {
	d.work++
	if d.work > maxComparisons {
		return errTooComplex
	}
	if d.work&1023 == 0 {
		return d.ctx.Err()
	}
	return nil
}
func (d *differ) emit(k editKind, l textLine) error {
	if len(d.edits) >= maxEdits {
		return errTooComplex
	}
	d.edits = append(d.edits, edit{k, l})
	return nil
}

type occurrence struct {
	text       string
	pos, count int
}

func lineKey(line textLine) string {
	if line.lf {
		return line.text + "\x00L"
	}
	return line.text + "\x00N"
}
func occurrences(lines []textLine) []occurrence {
	r := make([]occurrence, len(lines))
	for i, l := range lines {
		r[i] = occurrence{text: lineKey(l), pos: i, count: 1}
	}
	sort.Slice(r, func(i, j int) bool {
		if r[i].text == r[j].text {
			return r[i].pos < r[j].pos
		}
		return r[i].text < r[j].text
	})
	out := r[:0]
	for _, x := range r {
		if len(out) > 0 && out[len(out)-1].text == x.text {
			out[len(out)-1].count++
			continue
		}
		out = append(out, x)
	}
	return out
}
func uniquePos(r []occurrence, s string) (int, bool) {
	i := sort.Search(len(r), func(i int) bool { return r[i].text >= s })
	return func() (int, bool) {
		if i < len(r) && r[i].text == s && r[i].count == 1 {
			return r[i].pos, true
		}
		return 0, false
	}()
}
func (d *differ) segment(a, b []textLine) error {
	if err := d.ctx.Err(); err != nil {
		return err
	}
	if err := d.spendN(int64(len(a)+len(b)) * 14); err != nil {
		return err
	}
	ao, bo := occurrences(a), occurrences(b)
	pairs := make([][2]int, 0, min(len(a), len(b)))
	for i, x := range a {
		if err := d.spend(); err != nil {
			return err
		}
		key := lineKey(x)
		if _, ok := uniquePos(ao, key); ok {
			if p, ok := uniquePos(bo, key); ok {
				pairs = append(pairs, [2]int{i, p})
			}
		}
	}
	if err := d.spendN(int64(len(pairs)) * 14); err != nil {
		return err
	}
	tails := make([]int, 0, len(pairs))
	prev := make([]int, len(pairs))
	for i, p := range pairs {
		j := sort.Search(len(tails), func(j int) bool { return pairs[tails[j]][1] >= p[1] })
		prev[i] = -1
		if j > 0 {
			prev[i] = tails[j-1]
		}
		if j == len(tails) {
			tails = append(tails, i)
		} else {
			tails[j] = i
		}
	}
	anchors := make([][2]int, len(tails))
	if len(tails) > 0 {
		k := tails[len(tails)-1]
		for i := len(anchors) - 1; i >= 0; i-- {
			anchors[i] = pairs[k]
			k = prev[k]
		}
	}
	if len(anchors) == 0 {
		return d.hirschberg(a, b)
	}
	ai, bi := 0, 0
	for _, p := range anchors {
		if err := d.hirschberg(a[ai:p[0]], b[bi:p[1]]); err != nil {
			return err
		}
		if err := d.emit(equal, a[p[0]]); err != nil {
			return err
		}
		ai, bi = p[0]+1, p[1]+1
	}
	return d.hirschberg(a[ai:], b[bi:])
}
func (d *differ) row(a, b []textLine, reverse bool, out []int) error {
	n := len(b) + 1
	prev := d.scratch[:n]
	cur := d.scratch[n : 2*n]
	clear(prev)
	clear(cur)
	for i := range a {
		for j := range b {
			if err := d.spend(); err != nil {
				return err
			}
			ai, bj := i, j
			if reverse {
				ai = len(a) - 1 - i
				bj = len(b) - 1 - j
			}
			if a[ai].text == b[bj].text && a[ai].lf == b[bj].lf {
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
	copy(out, prev)
	return nil
}
func (d *differ) hirschberg(a, b []textLine) error {
	if err := d.ctx.Err(); err != nil {
		return err
	}
	if len(a) == 0 {
		for _, x := range b {
			if err := d.emit(addition, x); err != nil {
				return err
			}
		}
		return nil
	}
	if len(b) == 0 {
		for _, x := range a {
			if err := d.emit(deletion, x); err != nil {
				return err
			}
		}
		return nil
	}
	if len(a) == 1 {
		at := -1
		for j, x := range b {
			if err := d.spend(); err != nil {
				return err
			}
			if at < 0 && a[0] == x {
				at = j
			}
		}
		if at < 0 {
			if err := d.emit(deletion, a[0]); err != nil {
				return err
			}
			for _, x := range b {
				if err := d.emit(addition, x); err != nil {
					return err
				}
			}
			return nil
		}
		for _, x := range b[:at] {
			if err := d.emit(addition, x); err != nil {
				return err
			}
		}
		if err := d.emit(equal, a[0]); err != nil {
			return err
		}
		for _, x := range b[at+1:] {
			if err := d.emit(addition, x); err != nil {
				return err
			}
		}
		return nil
	}
	n := len(b) + 1
	left := d.scratch[2*n : 3*n]
	right := d.scratch[3*n : 4*n]
	clear(left)
	clear(right)
	mid := len(a) / 2
	if err := d.row(a[:mid], b, false, left); err != nil {
		return err
	}
	if err := d.row(a[mid:], b, true, right); err != nil {
		return err
	}
	cut, best := 0, -1
	for j := 0; j <= len(b); j++ {
		if v := left[j] + right[len(b)-j]; v > best {
			best = v
			cut = j
		}
	}
	if err := d.hirschberg(a[:mid], b[:cut]); err != nil {
		return err
	}
	return d.hirschberg(a[mid:], b[cut:])
}

func hunksFromEdits(edits []edit) ([]protocol.DiffHunk, error) {
	if len(edits) == 0 {
		return []protocol.DiffHunk{}, nil
	}
	changed := make([]int, 0)
	for i, e := range edits {
		if e.kind != equal {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return []protocol.DiffHunk{}, nil
	}
	ranges := make([][2]int, 0)
	start, end := max(0, changed[0]-3), min(len(edits), changed[0]+4)
	for _, i := range changed[1:] {
		s, e := max(0, i-3), min(len(edits), i+4)
		if s <= end {
			end = max(end, e)
		} else {
			ranges = append(ranges, [2]int{start, end})
			start, end = s, e
		}
	}
	ranges = append(ranges, [2]int{start, end})
	if len(ranges) > maxHunks {
		return nil, errTooComplex
	}
	oldNo, newNo := 1, 1
	positions := make([][2]int, len(edits)+1)
	for i, e := range edits {
		positions[i] = [2]int{oldNo, newNo}
		if e.kind != addition {
			oldNo++
		}
		if e.kind != deletion {
			newNo++
		}
	}
	out := make([]protocol.DiffHunk, 0, len(ranges))
	for _, r := range ranges {
		h := protocol.DiffHunk{Lines: make([]protocol.DiffLine, 0, r[1]-r[0])}
		os, ns := positions[r[0]][0], positions[r[0]][1]
		oc, nc := 0, 0
		for i := r[0]; i < r[1]; i++ {
			e := edits[i]
			ol, nl := positions[i][0], positions[i][1]
			line := protocol.DiffLine{Content: e.line.text, HasTerminatingLF: e.line.lf}
			switch e.kind {
			case equal:
				line.Kind = "context"
				line.OldLine = &ol
				line.NewLine = &nl
				oc++
				nc++
			case deletion:
				line.Kind = "deletion"
				line.OldLine = &ol
				oc++
			case addition:
				line.Kind = "addition"
				line.NewLine = &nl
				nc++
			}
			h.Lines = append(h.Lines, line)
		}
		h.OldCount, h.NewCount = oc, nc
		if oc == 0 {
			h.OldStart = 0
		} else {
			h.OldStart = os
		}
		if nc == 0 {
			h.NewStart = 0
		} else {
			h.NewStart = ns
		}
		out = append(out, h)
	}
	return out, nil
}
