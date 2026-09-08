package codingtools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

func TestRunCommandStopsWhenOwnerProcessDies(t *testing.T) {
	if os.Getenv("KIT_COMMAND_OWNER_HELPER") == "1" {
		marker := os.Getenv("KIT_COMMAND_OWNER_MARKER")
		_, _ = RunCommand(
			context.Background(), "bash",
			`trap '' TERM; printf started > "$KIT_COMMAND_OWNER_MARKER.started"; sleep 6; printf leaked > "$KIT_COMMAND_OWNER_MARKER"`,
			filepath.Dir(marker), 10*time.Second, 1024,
		)
		return
	}
	marker := filepath.Join(t.TempDir(), "finished")
	command := exec.Command(os.Args[0], "-test.run=^TestRunCommandStopsWhenOwnerProcessDies$")
	command.Env = append(os.Environ(),
		"KIT_COMMAND_OWNER_HELPER=1",
		"KIT_COMMAND_OWNER_MARKER="+marker,
	)
	if err := command.Start(); err != nil {
		t.Fatalf("start owner helper: %v", err)
	}
	started := marker + ".started"
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatal("owned command did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill owner helper: %v", err)
	}
	_ = command.Wait()
	time.Sleep(6500 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned command survived owner death: %v", err)
	}
}

func TestBaseToolDefinitions(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	names := []string{
		newBashTool(cwd).Name,
		newReadTool(cwd).Name,
		newWriteTool(cwd).Name,
		newEditTool(cwd).Name,
		newListTool(cwd).Name,
		newGrepTool(cwd).Name,
		newFindTool(cwd).Name,
	}
	want := []string{"bash", "read", "write", "edit", "ls", "grep", "find"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tool names = %v, want %v", names, want)
	}
	if got := len(New(cwd)); got != len(want) {
		t.Fatalf("New() returned %d tools, want %d", got, len(want))
	}
}

func TestDynamicCodingToolsReadCurrentCWDAtExecutionAdmission(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "scope.txt"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "scope.txt"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := first
	tool := newReadToolWithCWD(func() string { return cwd })
	result, err := tool.Execute(t.Context(), droids.ToolContext{}, readArgs{Path: "scope.txt"}, nil)
	if err != nil || resultText(t, result) != "first" {
		t.Fatalf("first read = %q, error = %v", resultText(t, result), err)
	}
	cwd = second
	result, err = tool.Execute(t.Context(), droids.ToolContext{}, readArgs{Path: "scope.txt"}, nil)
	if err != nil || resultText(t, result) != "second" {
		t.Fatalf("second read = %q, error = %v", resultText(t, result), err)
	}
}

func TestReadSelectsLinesAndBoundsOutput(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	path := filepath.Join(cwd, "sample.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	offset, limit := 2, 2
	result, err := newReadTool(cwd).Execute(context.Background(), droids.ToolContext{}, readArgs{Path: "sample.txt", Offset: &offset, Limit: &limit}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resultText(t, result); got != "two\nthree" {
		t.Fatalf("read text = %q, want %q", got, "two\nthree")
	}
	if details := decodeDetails[readDetails](t, result.Details); details.Path != path || details.Lines != 2 || details.Truncated {
		t.Fatalf("read details = %+v", details)
	}

	if err := os.WriteFile(path, []byte(strings.Repeat("é", maxReadOutputBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = newReadTool(cwd).Execute(context.Background(), droids.ToolContext{}, readArgs{Path: "sample.txt"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, result)
	if !strings.HasSuffix(text, "\n[truncated]") || strings.ToValidUTF8(text, "") != text {
		t.Fatalf("bounded read returned invalid or unmarked text")
	}
	if !decodeDetails[readDetails](t, result.Details).Truncated {
		t.Fatal("read details did not report truncation")
	}
}

func TestReadEmptyFileIsOneEmptyLine(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "empty"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := newReadTool(cwd).Execute(context.Background(), droids.ToolContext{}, readArgs{Path: "empty"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text := resultText(t, result); text != "" {
		t.Fatalf("empty read text = %q", text)
	}
	if lines := decodeDetails[readDetails](t, result.Details).Lines; lines != 1 {
		t.Fatalf("empty read lines = %d, want 1", lines)
	}
}

func TestWriteCreatesParents(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	content := "one\ntwo"
	result, err := newWriteTool(cwd).Execute(context.Background(), droids.ToolContext{}, writeArgs{Path: "nested/file.txt", Content: &content}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cwd, "nested", "file.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "one\ntwo" {
		t.Fatalf("written content = %q", body)
	}
	if text := resultText(t, result); text != "Wrote 2 lines to "+path {
		t.Fatalf("write result = %q", text)
	}

	missing, err := newWriteTool(cwd).Execute(context.Background(), droids.ToolContext{}, writeArgs{Path: "missing-content.txt"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !missing.IsError {
		t.Fatalf("missing-content write = %+v", missing)
	}
	if _, err := os.Stat(filepath.Join(cwd, "missing-content.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-content write created a file: %v", err)
	}
}

func TestEditAppliesAtomicExactReplacements(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	path := filepath.Join(cwd, "sample.txt")
	if err := os.WriteFile(path, []byte("alpha beta gamma"), 0o600); err != nil {
		t.Fatal(err)
	}
	alpha, a, gamma, g := "alpha", "A", "gamma", "G"
	result, err := newEditTool(cwd).Execute(context.Background(), droids.ToolContext{}, editArgs{
		Path:  "sample.txt",
		Edits: []editInput{{OldText: &alpha, NewText: &a}, {OldText: &gamma, NewText: &g}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || decodeDetails[editDetails](t, result.Details).Applied != 2 {
		t.Fatalf("edit result = %+v", result)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "A beta G" {
		t.Fatalf("edited content = %q", body)
	}

	oldA, changed, missing, x := "A", "changed", "missing", "x"
	result, err = newEditTool(cwd).Execute(context.Background(), droids.ToolContext{}, editArgs{
		Path:  "sample.txt",
		Edits: []editInput{{OldText: &oldA, NewText: &changed}, {OldText: &missing, NewText: &x}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(resultText(t, result), "oldText not found") {
		t.Fatalf("invalid edit result = %+v", result)
	}
	body, _ = os.ReadFile(path)
	if string(body) != "A beta G" {
		t.Fatalf("failed batch changed content to %q", body)
	}
}

func TestEditRejectsNonUniqueAndOverlappingMatches(t *testing.T) {
	t.Parallel()
	_, errors := applyExactEdits("aaaa", []exactEdit{{OldText: "aa", NewText: "x"}})
	if len(errors) != 1 || errors[0] != "edits[0]: oldText matches 3 locations — must be unique" {
		t.Fatalf("non-unique errors = %v", errors)
	}
	_, errors = applyExactEdits("abcdef", []exactEdit{{OldText: "abc", NewText: "x"}, {OldText: "bcd", NewText: "y"}})
	if len(errors) != 1 || !strings.Contains(errors[0], "overlap") {
		t.Fatalf("overlap errors = %v", errors)
	}
}

func TestListSortsAndMarksDirectories(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "z.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cwd, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := newListTool(cwd).Execute(context.Background(), droids.ToolContext{}, listArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text := resultText(t, result); text != "a/\nz.txt" {
		t.Fatalf("ls text = %q", text)
	}
}

func TestGlobExpansionIsBounded(t *testing.T) {
	t.Parallel()
	if _, err := compileGlob(strings.Repeat("{a,b}", 9), true); err == nil {
		t.Fatal("512-variant glob error = nil")
	}
}

func TestFindAndGrepRespectGitignore(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	writeTestFile(t, cwd, ".gitignore", "ignored/\n*.tmp\n")
	writeTestFile(t, cwd, "src/main.go", "before\nNeedle here\nafter\n")
	writeTestFile(t, cwd, "src/extra.tmp", "Needle hidden\n")
	writeTestFile(t, cwd, "ignored/no.go", "Needle hidden\n")
	writeTestFile(t, cwd, ".hidden/visible.go", "needle hidden file\n")

	findResult, err := newFindTool(cwd).Execute(context.Background(), droids.ToolContext{}, findArgs{Pattern: "**/*.go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resultText(t, findResult); got != ".hidden/visible.go\nsrc/main.go" {
		t.Fatalf("find result = %q", got)
	}
	braceResult, err := newFindTool(cwd).Execute(context.Background(), droids.ToolContext{}, findArgs{Pattern: "**/*.{go,tmp}"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resultText(t, braceResult); got != ".hidden/visible.go\nsrc/main.go" {
		t.Fatalf("brace find result = %q", got)
	}

	contextLines := 1
	grepResult, err := newGrepTool(cwd).Execute(context.Background(), droids.ToolContext{}, grepArgs{
		Pattern: "needle", Glob: "*.go", IgnoreCase: true, Context: &contextLines,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := ".hidden/visible.go:1: needle hidden file\n.hidden/visible.go:2- \nsrc/main.go:1- before\nsrc/main.go:2: Needle here\nsrc/main.go:3- after"
	if got := resultText(t, grepResult); got != want {
		t.Fatalf("grep result:\n%s\nwant:\n%s", got, want)
	}
	if details := decodeDetails[grepDetails](t, grepResult.Details); details.MatchCount != 2 {
		t.Fatalf("grep details = %+v", details)
	}
}

func TestGrepBoundsModelAndLiveOutput(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	line := "match " + strings.Repeat("x", 1_000)
	writeTestFile(t, cwd, "large.txt", strings.Repeat(line+"\n", 200))
	limit := 1_000
	result, err := newGrepTool(cwd).Execute(context.Background(), droids.ToolContext{}, grepArgs{Pattern: "match", Limit: &limit}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !decodeDetails[grepDetails](t, result.Details).BytesTruncated {
		t.Fatalf("grep details = %+v, want byte truncation", result.Details)
	}
	if got := len(resultText(t, result)); got >= 64<<10 {
		t.Fatalf("grep output = %d bytes, want less than live event cap", got)
	}
}

func TestMutationLockAcquisitionIsCancellable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locked")
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withMutationLock(context.Background(), path, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := withMutationLock(ctx, path, func() error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("lock error = %v, want context canceled", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("lock holder error = %v", err)
	}
}

func TestBashCapturesOutputExitAndTimeout(t *testing.T) {
	cwd := t.TempDir()
	result, err := newBashTool(cwd).Execute(context.Background(), droids.ToolContext{}, bashArgs{Command: "printf out; exit 3"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resultText(t, result); got != "out\n[exit code: 3]" {
		t.Fatalf("bash result = %q", got)
	}
	if code := decodeDetails[bashDetails](t, result.Details).ExitCode; code == nil || *code != 3 {
		t.Fatalf("bash exit details = %+v", result.Details)
	}

	timeout := int64(20)
	started := time.Now()
	result, err = newBashTool(cwd).Execute(context.Background(), droids.ToolContext{}, bashArgs{Command: "sleep 5", Timeout: &timeout}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("bash timeout took %v", time.Since(started))
	}
	if got := resultText(t, result); got != "[timed out]" {
		t.Fatalf("timed out bash result = %q", got)
	}
	if !decodeDetails[bashDetails](t, result.Details).TimedOut {
		t.Fatalf("timeout details = %+v", result.Details)
	}
}

func TestToolsHonorCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newBashTool(t.TempDir()).Execute(ctx, droids.ToolContext{}, bashArgs{Command: "printf no"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("bash error = %v, want context canceled", err)
	}
}

func resultText(t *testing.T, result droids.ToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("result content = %+v", result.Content)
	}
	text, ok := result.Content[0].(droids.TextContent)
	if !ok {
		t.Fatalf("result content type = %T", result.Content[0])
	}
	return text.Text
}

func decodeDetails[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var details T
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	return details
}

func writeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
