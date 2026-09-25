package boundeddiff

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

var generous = Limits{MaxWork: 20_000_000, MaxMemoryBytes: 16 << 20, MaxEdits: 20_000}

func reconstruct(edits []Edit) (old, new []string) {
	for _, e := range edits {
		switch e.Kind {
		case Equal:
			old = append(old, e.Text)
			new = append(new, e.Text)
		case Delete:
			old = append(old, e.Text)
		case Insert:
			new = append(new, e.Text)
		}
	}
	return
}
func TestReconstruction(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	alphabet := []string{"a\n", "b\n", "c\n", "same\n"}
	for n := 0; n < 500; n++ {
		a := make([]string, r.Intn(14))
		b := make([]string, r.Intn(14))
		for i := range a {
			a[i] = alphabet[r.Intn(len(alphabet))]
		}
		for i := range b {
			b[i] = alphabet[r.Intn(len(alphabet))]
		}
		got, err := Diff(t.Context(), a, b, generous)
		if err != nil {
			t.Fatal(err)
		}
		ao, bo := reconstruct(got)
		if strings.Join(ao, "") != strings.Join(a, "") || strings.Join(bo, "") != strings.Join(b, "") {
			t.Fatalf("reconstruction failed: %#v", got)
		}
		cost := 0
		for _, e := range got {
			if e.Kind != Equal {
				cost++
			}
		}
		if cost > len(a)+len(b) {
			t.Fatalf("invalid cost=%d a=%q b=%q", cost, a, b)
		}
	}
}
func TestAdversarialAndBounds(t *testing.T) {
	repeated := make([]string, 1500)
	for i := range repeated {
		repeated[i] = "x\n"
	}
	shifted := append([]string{"y\n"}, repeated[:1499]...)
	if _, err := Diff(t.Context(), repeated, shifted, generous); err != nil {
		t.Fatal(err)
	}
	a, b := make([]string, 5000), make([]string, 5000)
	for i := range a {
		a[i] = fmt.Sprintf("a-%d\n", i)
		b[i] = fmt.Sprintf("b-%d\n", i)
	}
	if _, err := Diff(t.Context(), a, b, Limits{MaxWork: 2_000_000, MaxMemoryBytes: 100_000, MaxEdits: 20_000}); !errors.Is(err, ErrTooComplex) {
		t.Fatalf("low-memory error=%v", err)
	}
	_, err := Diff(t.Context(), a, b, Limits{MaxWork: 2_000_000, MaxMemoryBytes: 16 << 20, MaxEdits: 20_000})
	if !errors.Is(err, ErrTooComplex) {
		t.Fatalf("disjoint error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Diff(ctx, a, b, generous); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
}

func TestDifferentialAgainstSanitizedGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	cases := [][2][]string{{{"a\n", "b\n"}, {"a\n", "c\n"}}, {{"repeat\n", "repeat\n", "z"}, {"repeat\n", "z\n", "repeat\n"}}, {{}, {"new\n"}}}
	for _, tc := range cases {
		edits, err := Diff(t.Context(), tc[0], tc[1], generous)
		if err != nil {
			t.Fatal(err)
		}
		adds, dels := 0, 0
		for _, e := range edits {
			if e.Kind == Insert {
				adds++
			}
			if e.Kind == Delete {
				dels++
			}
		}
		ga, gd := gitCounts(t, tc[0], tc[1])
		if adds != ga || dels != gd {
			t.Fatalf("counts=(%d,%d), git=(%d,%d)", adds, dels, ga, gd)
		}
	}
}
func gitCounts(t *testing.T, a, b []string) (int, int) {
	t.Helper()
	d := t.TempDir()
	old, newp := d+"/old", d+"/new"
	if err := os.WriteFile(old, []byte(strings.Join(a, "")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newp, []byte(strings.Join(b, "")), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "--no-pager", "diff", "--no-index", "--numstat", "--no-ext-diff", "--no-textconv", "--", old, newp)
	cmd.Dir = d
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + d, "XDG_CONFIG_HOME=" + d, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	out, err := cmd.Output()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		if err != nil {
			t.Fatal(err)
		}
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		if strings.Join(a, "") == strings.Join(b, "") {
			return 0, 0
		}
		t.Fatalf("git output %q", out)
	}
	adds, _ := strconv.Atoi(fields[0])
	dels, _ := strconv.Atoi(fields[1])
	return adds, dels
}

func BenchmarkFiveThousandSmallChange(b *testing.B) {
	a := make([]string, 5000)
	for i := range a {
		a[i] = fmt.Sprintf("line-%d\n", i)
	}
	n := append([]string(nil), a...)
	n[2500] = "changed\n"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Diff(context.Background(), a, n, generous); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkFiveThousandDisjointBounded(b *testing.B) {
	a, n := make([]string, 5000), make([]string, 5000)
	for i := range a {
		a[i] = fmt.Sprintf("a-%d\n", i)
		n[i] = fmt.Sprintf("b-%d\n", i)
	}
	lim := Limits{MaxWork: 2_000_000, MaxMemoryBytes: 16 << 20, MaxEdits: 20_000}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Diff(context.Background(), a, n, lim); !errors.Is(err, ErrTooComplex) {
			b.Fatal(err)
		}
	}
}

func FuzzReconstruct(f *testing.F) {
	f.Add("a\nb\n", "a\nc\n")
	f.Add("x\nx\n", "x\n")
	f.Fuzz(func(t *testing.T, a, b string) {
		if len(a) > 200 || len(b) > 200 {
			return
		}
		old, newp := split(a), split(b)
		edits, err := Diff(t.Context(), old, newp, generous)
		if err != nil {
			t.Fatal(err)
		}
		ao, bo := reconstruct(edits)
		if strings.Join(ao, "") != a || strings.Join(bo, "") != b {
			t.Fatal("reconstruction")
		}
	})
}
func split(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return append(out, s)
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}
