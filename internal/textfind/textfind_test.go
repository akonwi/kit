package textfind

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fixture struct {
	Cases []struct {
		Name      string `json:"name"`
		Query     string `json:"query"`
		MatchCase bool   `json:"matchCase"`
		WholeWord bool   `json:"wholeWord"`
		Limit     int    `json:"limit"`
		Capped    bool   `json:"capped"`
		Current   *int   `json:"current"`
		Marked    string `json:"marked"`
	} `json:"cases"`
	Labels []struct {
		Current       int    `json:"current"`
		Total         int    `json:"total"`
		Capped        bool   `json:"capped"`
		SearchedLines int    `json:"searchedLines"`
		Label         string `json:"label"`
	} `json:"labels"`
}

// readFixture reads testdata/find/cases.json, which every client's matcher
// reads.
func readFixture(t *testing.T) fixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "find", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases fixture
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

// unmark removes the fixture's markers from marked, returning the text,
// the byte ranges between ⟦ and ⟧, and the byte offset of ‸ (-1 if none).
func unmark(marked string) (string, []Range, int) {
	var text strings.Builder
	var ranges []Range
	anchor := -1
	for _, r := range marked {
		switch r {
		case '⟦':
			ranges = append(ranges, Range{Start: text.Len()})
		case '⟧':
			ranges[len(ranges)-1].End = text.Len()
		case '‸':
			anchor = text.Len()
		default:
			text.WriteRune(r)
		}
	}
	return text.String(), ranges, anchor
}

func TestFindAgreesWithTheSharedCases(t *testing.T) {
	t.Parallel()
	for _, c := range readFixture(t).Cases {
		t.Run(c.Name, func(t *testing.T) {
			text, want, anchor := unmark(c.Marked)
			got := Find(text, c.Query, Options{MatchCase: c.MatchCase, WholeWord: c.WholeWord, Limit: c.Limit})
			if !reflect.DeepEqual(got.Matches, want) {
				t.Errorf("Find(%q, %q) matches = %v, want %v", text, c.Query, got.Matches, want)
			}
			if got.Capped != c.Capped {
				t.Errorf("Find(%q, %q) capped = %v, want %v", text, c.Query, got.Capped, c.Capped)
			}
			if c.Current != nil {
				if current := Following(got.Matches, anchor); current != *c.Current {
					t.Errorf("Following(%v, %d) = %d, want %d", got.Matches, anchor, current, *c.Current)
				}
			}
		})
	}
}

func TestLabelAgreesWithTheSharedCases(t *testing.T) {
	t.Parallel()
	for _, c := range readFixture(t).Labels {
		if got := Label(c.Current, c.Total, c.Capped, c.SearchedLines); got != c.Label {
			t.Errorf("Label(%d, %d, %v, %d) = %q, want %q", c.Current, c.Total, c.Capped, c.SearchedLines, got, c.Label)
		}
	}
}

func TestFindCountsTenThousandMatchesByDefault(t *testing.T) {
	t.Parallel()
	exactly := strings.Repeat("ab ", DefaultLimit)
	got := Find(exactly, "ab", Options{})
	if len(got.Matches) != DefaultLimit || got.Capped {
		t.Fatalf("Find on %d matches = %d matches, capped %v; want %d, not capped", DefaultLimit, len(got.Matches), got.Capped, DefaultLimit)
	}
	if last := got.Matches[DefaultLimit-1]; last != (Range{Start: 3 * (DefaultLimit - 1), End: 3*(DefaultLimit-1) + 2}) {
		t.Fatalf("last match = %v", last)
	}
	got = Find(exactly+"ab", "ab", Options{})
	if len(got.Matches) != DefaultLimit || !got.Capped {
		t.Fatalf("Find on %d matches = %d matches, capped %v; want %d, capped", DefaultLimit+1, len(got.Matches), got.Capped, DefaultLimit)
	}
}

func TestFollowingWithoutMatchesIsNone(t *testing.T) {
	t.Parallel()
	if got := Following(nil, 0); got != -1 {
		t.Fatalf("Following(nil, 0) = %d, want -1", got)
	}
}

// BenchmarkFindInALargeFile searches a file at the size clients receive
// at most: 5,000 lines of about 200 bytes.
func BenchmarkFindInALargeFile(b *testing.B) {
	line := strings.Repeat("func (s *Server) handler(next http.Handler) http.Handler { return next } ", 3)
	text := strings.Repeat(line+"\n", 5000)
	for _, query := range []string{"handler", "e", "zzz", "Handler(next http.Handler) http.Handler {"} {
		b.Run(query, func(b *testing.B) {
			for b.Loop() {
				Find(text, query, Options{WholeWord: true})
			}
		})
	}
}
