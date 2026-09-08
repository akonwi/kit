package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceReadsDoNotWaitForMutationIO(t *testing.T) {
	scope := newWorkspaceScope("/before")
	scope.mutationMu.Lock()
	defer scope.mutationMu.Unlock()

	read := make(chan string, 1)
	go func() { read <- scope.CWD() }()
	select {
	case cwd := <-read:
		if cwd != "/before" {
			t.Fatalf("CWD() = %q, want /before", cwd)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("CWD() blocked behind a mutation")
	}
}

func TestWorkspacePublishesCWDAndGenerationTogether(t *testing.T) {
	scope := newWorkspaceScope("/before")
	scope.mutationMu.Lock()
	scope.publish("/after")
	scope.mutationMu.Unlock()
	cwd, generation := scope.snapshot()
	if cwd != "/after" || generation != 1 {
		t.Fatalf("snapshot() = (%q, %d), want (/after, 1)", cwd, generation)
	}
}

func TestResolveCWDTargetSupportsRelativeAbsoluteAndHomePaths(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "workspace", "project")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		target string
		want   string
	}{
		{name: "relative", target: "../other", want: filepath.Join(string(filepath.Separator), "workspace", "other")},
		{name: "absolute", target: filepath.Join(string(filepath.Separator), "tmp", "other"), want: filepath.Join(string(filepath.Separator), "tmp", "other")},
		{name: "home", target: "~", want: home},
		{name: "home child", target: "~/code", want: filepath.Join(home, "code")},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveCWDTarget(base, test.target)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("resolveCWDTarget() = %q, want %q", got, test.want)
			}
		})
	}
	for _, invalid := range []string{"", "   ", "bad\x00path"} {
		if _, err := resolveCWDTarget(base, invalid); err == nil {
			t.Fatalf("resolveCWDTarget(%q) succeeded", invalid)
		}
	}
}
