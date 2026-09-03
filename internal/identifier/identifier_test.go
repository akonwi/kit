package identifier

import (
	"regexp"
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()

	first, err := New("session_")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	second, err := New("session_")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if first == second {
		t.Fatalf("New() returned duplicate %q", first)
	}
	if !regexp.MustCompile(`^session_[0-9a-f]{32}$`).MatchString(first) {
		t.Fatalf("identifier = %q", first)
	}
	if !Valid(first, "session_") || Valid(first, "run_") || Valid("session_bad", "session_") {
		t.Fatalf("Valid() rejected or confused identifier %q", first)
	}
}
