package workingdiff

import (
	"context"
	"errors"
	"github.com/akonwi/kit/internal/protocol"
	"math/rand"
	"strings"
	"testing"
)

func TestDifferReconstructsAndPreservesLF(t *testing.T) {
	r := rand.New(rand.NewSource(23))
	alphabet := []string{"a\n", "b\r\n", "c", "same\n"}
	for n := 0; n < 500; n++ {
		a, b := "", ""
		for range r.Intn(20) {
			a += alphabet[r.Intn(len(alphabet))]
		}
		for range r.Intn(20) {
			b += alphabet[r.Intn(len(alphabet))]
		}
		al, _ := splitLines([]byte(a))
		bl, _ := splitLines([]byte(b))
		ed, e := semanticDiff(t.Context(), al, bl)
		if e != nil {
			t.Fatal(e)
		}
		var ao, bo strings.Builder
		for _, x := range ed {
			line := x.line.text
			if x.line.lf {
				line += "\n"
			}
			if x.kind != addition {
				ao.WriteString(line)
			}
			if x.kind != deletion {
				bo.WriteString(line)
			}
		}
		if ao.String() != a || bo.String() != b {
			t.Fatalf("reconstruction %q/%q", ao.String(), bo.String())
		}
	}
}
func TestDifferBoundsCancellationAndHunks(t *testing.T) {
	a, b := make([]textLine, protocol.MaxDiffFileLines), make([]textLine, protocol.MaxDiffFileLines)
	for i := range a {
		a[i] = textLine{"a", true}
		b[i] = textLine{"b", true}
	}
	if _, e := semanticDiff(t.Context(), a, b); !errors.Is(e, errTooComplex) {
		t.Fatalf("error=%v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := semanticDiff(ctx, a[:100], b[:100]); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel=%v", e)
	}
	ed, e := semanticDiff(t.Context(), []textLine{{"old\r", true}, {"tail", false}}, []textLine{{"new\r", true}, {"tail", false}})
	if e != nil {
		t.Fatal(e)
	}
	h, e := hunksFromEdits(ed)
	if e != nil || len(h) != 1 {
		t.Fatalf("hunks=%v %v", h, e)
	}
	if h[0].Lines[0].Content != "old\r" || !h[0].Lines[0].HasTerminatingLF {
		t.Fatal("CR/LF state lost")
	}
	if h[0].Lines[len(h[0].Lines)-1].HasTerminatingLF {
		t.Fatal("final LF state lost")
	}
}
func FuzzDifferReconstruct(f *testing.F) {
	f.Add("a\nb\n", "a\nc")
	f.Add("x\r\n", "x\n")
	f.Fuzz(func(t *testing.T, a, b string) {
		if len(a) > 300 || len(b) > 300 {
			return
		}
		al, ar := splitLines([]byte(a))
		bl, br := splitLines([]byte(b))
		if ar != "" || br != "" {
			return
		}
		ed, e := semanticDiff(t.Context(), al, bl)
		if e != nil {
			return
		}
		var ao, bo strings.Builder
		for _, x := range ed {
			v := x.line.text
			if x.line.lf {
				v += "\n"
			}
			if x.kind != addition {
				ao.WriteString(v)
			}
			if x.kind != deletion {
				bo.WriteString(v)
			}
		}
		if ao.String() != a || bo.String() != b {
			t.Fatal("reconstruction")
		}
	})
}
