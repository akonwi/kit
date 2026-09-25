//go:build darwin || linux

package childproc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartRejectsInvalidSpec(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		spec Spec
		want string
	}{
		{name: "no executable", spec: Spec{Dir: root}, want: "child process requires an executable"},
		{name: "empty working directory", spec: Spec{Path: "/bin/echo"}, want: "working directory must be absolute"},
		{name: "relative working directory", spec: Spec{Path: "/bin/echo", Dir: "relative"}, want: "working directory must be absolute"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Start(t.Context(), testCase.spec)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("err = %v, want one containing %q", err, testCase.want)
			}
		})
	}
}

func TestStartRejectsCancelledContextBeforeLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Start(ctx, Spec{Path: "/bin/echo", Dir: t.TempDir()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestStartReportsMissingExecutable(t *testing.T) {
	_, err := Start(t.Context(), Spec{Path: filepath.Join(t.TempDir(), "missing"), Dir: t.TempDir()})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

// An explicit empty environment must not silently fall back to inheritance,
// because that distinction is Kit's trust boundary for third-party servers.
func TestStartEnvironmentPolicy(t *testing.T) {
	t.Setenv("KIT_CHILDPROC_INHERITED_TEST", "inherited-value")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestChildProcessHelper$", "--", "describe"}

	for _, testCase := range []struct {
		name string
		env  []string
		want string
	}{
		{name: "nil inherits the parent environment", env: nil, want: "inherited-value"},
		{name: "empty is not inheritance", env: []string{}, want: ""},
		{name: "explicit value replaces inheritance", env: []string{"KIT_CHILDPROC_INHERITED_TEST=explicit-value"}, want: "explicit-value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := testCase.env
			if env != nil {
				env = append(env, helperMarker+"=1")
			} else {
				t.Setenv(helperMarker, "1")
			}
			process, err := Start(t.Context(), Spec{Path: executable, Args: args, Dir: t.TempDir(), Env: env})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = process.Stop(ctx)
			})
			var got struct {
				Environment string `json:"environment"`
			}
			if err := json.NewDecoder(process.Output()).Decode(&got); err != nil {
				t.Fatalf("decode: %v; stderr = %q", err, process.Stderr())
			}
			if got.Environment != testCase.want {
				t.Fatalf("environment = %q, want %q", got.Environment, testCase.want)
			}
		})
	}
}

func TestStartHonorsStderrLimitOverride(t *testing.T) {
	t.Setenv(helperMarker, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process, err := Start(t.Context(), Spec{
		Path:        executable,
		Args:        []string{"-test.run=^TestChildProcessHelper$", "--", "stderr"},
		Dir:         t.TempDir(),
		StderrLimit: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// The helper writes its full stderr burst before announcing readiness, so this
	// asserts the retained tail rather than whatever happened to arrive first.
	if line, err := bufio.NewReader(process.Output()).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("readiness = %q, %v", line, err)
	}
	if err := process.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("x", 32-len("diagnostic-tail")) + "diagnostic-tail"
	if got := process.Stderr(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}
