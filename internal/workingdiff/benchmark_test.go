package workingdiff

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkDifferSmallChange(b *testing.B) {
	old := make([]textLine, 5000)
	for i := range old {
		old[i] = textLine{fmt.Sprintf("line-%d", i), true}
	}
	newLines := append([]textLine(nil), old...)
	newLines[2500] = textLine{"changed", true}
	b.ReportAllocs()
	for b.Loop() {
		if _, e := semanticDiff(context.Background(), old, newLines); e != nil {
			b.Fatal(e)
		}
	}
}
func BenchmarkDifferDisjointBounded(b *testing.B) {
	old, newLines := make([]textLine, 5000), make([]textLine, 5000)
	for i := range old {
		old[i] = textLine{fmt.Sprintf("a-%d", i), true}
		newLines[i] = textLine{fmt.Sprintf("b-%d", i), true}
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = semanticDiff(context.Background(), old, newLines)
	}
}
