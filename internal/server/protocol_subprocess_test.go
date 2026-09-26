package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/version"
	"github.com/gofrs/flock"
)

// TestSubprocessProtocol40ReleaseGate tests separately compiled binaries from
// the same source. The current release gate rejects skew without disruption;
// this is not evidence of compatibility across source revisions.
func TestSubprocessProtocol40ReleaseGate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess builds in short mode")
	}
	root := protocolTestRoot(t)
	runProtocolBinaryMatrix(t, [2]string{"release-a", "release-b"}, [2]string{root, root}, true, false)
}

// TestProtocol40TaggedReleaseMatrix checks bidirectional attachment with real
// release labels. It must never use the development-version bypass.
func TestProtocol40TaggedReleaseMatrix(t *testing.T) {
	runProtocol40TaggedMatrix(t, false)
}

// TestProtocol40TaggedWireSmoke uses dev-labeled *test clients* built from
// pinned revisions to inspect wire interoperability behind the current release
// gate. This is not a release compatibility pass and cannot replace the
// release-labeled matrix. Neither smoke test covers every protocol semantic.
func TestProtocol40TaggedWireSmoke(t *testing.T) {
	runProtocol40TaggedMatrix(t, true)
}

// Run with KIT_PROTOCOL40_BASELINE_TAG, KIT_PROTOCOL40_CANDIDATE_VERSION, and
// either KIT_PROTOCOL40_CANDIDATE_TAG or KIT_PROTOCOL40_CANDIDATE_COMMIT (a full
// SHA). A commit candidate remains provisional until that commit is released.
func runProtocol40TaggedMatrix(t *testing.T, wireOnly bool) {
	t.Helper()
	baseline := os.Getenv("KIT_PROTOCOL40_BASELINE_TAG")
	candidate := os.Getenv("KIT_PROTOCOL40_CANDIDATE_VERSION")
	candidateTag := os.Getenv("KIT_PROTOCOL40_CANDIDATE_TAG")
	candidateCommit := os.Getenv("KIT_PROTOCOL40_CANDIDATE_COMMIT")
	if baseline == "" && candidate == "" && candidateTag == "" && candidateCommit == "" {
		t.Skip("requires a tagged protocol-40 baseline and candidate release version")
	}
	if testing.Short() {
		t.Skip("skipping subprocess builds in short mode")
	}
	if baseline == "" || candidate == "" || baseline == candidate ||
		!regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$`).MatchString(candidate) ||
		(candidateTag != "" && candidateTag != candidate) || (candidateTag == "") == (candidateCommit == "") {
		t.Fatal("set a tagged baseline, distinct semver candidate, and exactly one matching candidate tag or full commit")
	}
	root := protocolTestRoot(t)
	candidateRef := candidateCommit
	if candidateTag != "" {
		candidateRef = "refs/tags/" + candidateTag
	}
	if protocolRevisionCommit(t, root, "refs/tags/"+baseline) == protocolRevisionCommit(t, root, candidateRef) {
		t.Fatal("baseline and candidate resolve to the same source revision")
	}
	baselineRoot := archiveProtocolTag(t, root, baseline)
	candidateRoot := ""
	if candidateTag != "" {
		candidateRoot = archiveProtocolTag(t, root, candidateTag)
	} else {
		candidateRoot = archiveProtocolCommit(t, root, candidateCommit)
	}
	if wireOnly {
		t.Log("WIRE-ONLY SMOKE: dev-labeled test clients bypass the release-string gate; not a release compatibility pass")
	}
	runProtocolBinaryMatrix(t, [2]string{baseline, candidate}, [2]string{baselineRoot, candidateRoot}, false, wireOnly)
}

// protocolSubprocessEnv prevents ambient matrix/helper controls from leaking
// into daemon and client subprocesses. The isolated home and dummy credential
// are always provided explicitly by callers.
func protocolSubprocessEnv(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "KIT_PROTOCOL") || key == apphome.EnvHome || key == "OPENAI_API_KEY" {
			continue
		}
		env = append(env, entry)
	}
	return append(env, extra...)
}

func protocolTestRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
}

func archiveProtocolTag(t *testing.T, root, tag string) string {
	t.Helper()
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$`).MatchString(tag) {
		t.Fatalf("protocol matrix requires a release tag, got %q", tag)
	}
	return archiveProtocolRevision(t, root, "refs/tags/"+tag, tag)
}

func archiveProtocolCommit(t *testing.T, root, commit string) string {
	t.Helper()
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(commit) {
		t.Fatalf("protocol matrix requires a full candidate commit SHA, got %q", commit)
	}
	return archiveProtocolRevision(t, root, commit, commit)
}

func protocolRevisionCommit(t *testing.T, root, ref string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", ref+"^{commit}")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve revision %s: %v\n%s", ref, err, out)
	}
	return strings.TrimSpace(string(out))
}

func archiveProtocolRevision(t *testing.T, root, ref, label string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	archive := filepath.Join(t.TempDir(), "source.tar")
	cmd := exec.CommandContext(ctx, "git", "archive", "-o", archive, ref)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("archive %s: %v\n%s", label, err, out)
	}
	sourceRoot := t.TempDir()
	extract := exec.CommandContext(ctx, "tar", "-xf", archive, "-C", sourceRoot)
	if out, err := extract.CombinedOutput(); err != nil {
		t.Fatalf("extract %s: %v\n%s", label, err, out)
	}
	metadata, err := os.ReadFile(filepath.Join(sourceRoot, "internal/version/version.go"))
	if err != nil {
		t.Fatalf("read protocol version for %s: %v", label, err)
	}
	if !regexp.MustCompile(`(?m)^\s*SessionProtocolVersion\s*=\s*40\s*$`).Match(metadata) {
		t.Fatalf("revision %s does not declare session protocol 40", label)
	}
	// Compile one identical client contract against each release's own types
	// and implementation. Keep the archive's runtime source unmodified.
	fixture, err := os.ReadFile(filepath.Join(root, "internal/server/protocol_subprocess_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "internal/server/protocol_subprocess_test.go"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	return sourceRoot
}

func runProtocolBinaryMatrix(t *testing.T, labels [2]string, roots [2]string, expectSkew, wireOnly bool) {
	t.Helper()
	binDir := t.TempDir()
	for index, label := range labels {
		root := roots[index]
		buildVersion := strings.TrimPrefix(label, "v")
		buildKitBinary(t, root, filepath.Join(binDir, "kit-"+label), buildVersion)
		clientVersion := buildVersion
		if wireOnly {
			// The pinned daemon binaries retain their real release labels. Only
			// the test clients exercise the existing development bypass.
			clientVersion = "dev"
		}
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		cmd := exec.CommandContext(ctx, "go", "test", "-c", "-ldflags", "-X github.com/akonwi/kit/internal/version.Version="+clientVersion,
			"-o", filepath.Join(binDir, "client-"+label), "./internal/server")
		cmd.Dir = root
		cmd.Env = protocolSubprocessEnv("GOTOOLCHAIN=auto", apphome.EnvHome+"="+t.TempDir())
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("build %s client: %v\n%s", label, err, out)
		}
	}
	for index, daemonLabel := range labels {
		t.Run(daemonLabel, func(t *testing.T) {
			root := roots[index]
			paths := apphome.FromHome(filepath.Join(t.TempDir(), "home"))
			workspace := t.TempDir()
			// The dummy credential exposes a local model catalog; this test never
			// sends a model request or connects to a remote provider.
			env := protocolSubprocessEnv(apphome.EnvHome+"="+paths.Home, "OPENAI_API_KEY=protocol-test-only")
			daemon := filepath.Join(binDir, "kit-"+daemonLabel)
			run := func(binary string, args ...string) (string, error) {
				t.Helper()
				// Cleanup runs after t.Context is canceled; use an independent
				// bounded context so the test daemon is actually stopped.
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Dir = root
				cmd.Env = env
				out, err := cmd.CombinedOutput()
				return string(out), err
			}
			if out, err := run(daemon, "server", "start"); err != nil {
				t.Fatalf("start daemon: %v\n%s", err, out)
			}
			var before Registry
			t.Cleanup(func() {
				if out, stopErr := run(daemon, "server", "stop"); stopErr != nil {
					t.Errorf("stop test daemon: %v\n%s", stopErr, out)
				}
				lifetime := flock.New(paths.ServerLock)
				deadline := time.Now().Add(3 * time.Second)
				for {
					available, lockErr := lifetime.TryLock()
					if lockErr != nil {
						t.Errorf("inspect test daemon lock: %v", lockErr)
						return
					}
					if available {
						if err := lifetime.Unlock(); err != nil {
							t.Errorf("release test daemon lock: %v", err)
						}
						return
					}
					if time.Now().After(deadline) {
						// Only kill the process if it still owns this isolated
						// registration; never target a potentially recycled PID.
						if current, err := LoadRegistry(paths); err == nil && current.InstanceID == before.InstanceID && current.PID == before.PID {
							if process, err := os.FindProcess(before.PID); err != nil {
								t.Errorf("find stuck test daemon: %v", err)
							} else if err := process.Kill(); err != nil {
								t.Errorf("kill stuck test daemon: %v", err)
							}
						}
						killDeadline := time.Now().Add(3 * time.Second)
						for time.Now().Before(killDeadline) {
							available, err := lifetime.TryLock()
							if err != nil {
								t.Errorf("inspect test daemon lock after kill: %v", err)
								break
							}
							if available {
								_ = lifetime.Unlock()
								t.Error("test daemon required forced termination")
								return
							}
							time.Sleep(25 * time.Millisecond)
						}
						t.Error("test daemon did not release lifetime lock even after forced termination")
						return
					}
					time.Sleep(25 * time.Millisecond)
				}
			})
			before, err := LoadRegistry(paths)
			if err != nil || before.KitVersion != strings.TrimPrefix(daemonLabel, "v") || before.ProtocolVersion != version.SessionProtocolVersion {
				t.Fatalf("daemon registry = %+v, %v", before, err)
			}
			other := labels[1-index]
			for _, clientLabel := range []string{daemonLabel, other} {
				client := filepath.Join(binDir, "client-"+clientLabel)
				clientEnv := append(append([]string{}, env...), "KIT_PROTOCOL_CLIENT_HELPER=1",
					"KIT_PROTOCOL_TEST_HOME="+paths.Home, "KIT_PROTOCOL_TEST_WORKSPACE="+workspace)
				if expectSkew && clientLabel != daemonLabel {
					clientEnv = append(clientEnv, "KIT_PROTOCOL_EXPECT_RELEASE_MISMATCH=1")
				} else if clientLabel == daemonLabel {
					observer := client
					if !expectSkew {
						observer = filepath.Join(binDir, "client-"+other)
					}
					clientEnv = append(clientEnv, "KIT_PROTOCOL_OBSERVER_BIN="+observer)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				cmd := exec.CommandContext(ctx, client, "-test.run=^TestProtocolClientHelper$", "-test.v")
				cmd.Dir = root
				cmd.Env = clientEnv
				out, runErr := cmd.CombinedOutput()
				cancel()
				after, err := LoadRegistry(paths)
				if err != nil || after.PID != before.PID || after.InstanceID != before.InstanceID || after.KitVersion != before.KitVersion {
					t.Fatalf("client %s replaced daemon: before=%+v after=%+v err=%v", clientLabel, before, after, err)
				}
				if runErr != nil {
					t.Fatalf("%s client against %s daemon: %v\n%s", clientLabel, daemonLabel, runErr, out)
				}
			}
		})
	}
}

// TestProtocolClientHelper runs only in a separately compiled test executable.
// It covers admission, mutation, snapshot, paged events and SSE over real HTTP.
func TestProtocolClientHelper(t *testing.T) {
	if os.Getenv("KIT_PROTOCOL_CLIENT_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	home := os.Getenv(apphome.EnvHome)
	if home == "" || home != os.Getenv("KIT_PROTOCOL_TEST_HOME") {
		t.Fatal("protocol subprocess requires an explicit isolated KIT_HOME")
	}
	paths := apphome.FromHome(home)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	registry, err := NewManager(paths).Ensure(ctx)
	if os.Getenv("KIT_PROTOCOL_EXPECT_RELEASE_MISMATCH") == "1" {
		var mismatch *DaemonCompatibilityError
		if !errors.As(err, &mismatch) || mismatch.Reason != ReleaseMismatch ||
			mismatch.ClientVersion != version.Version || mismatch.DaemonVersion == version.Version {
			t.Fatalf("release skew = %v, want typed non-destructive release mismatch", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if registry.ProtocolVersion != version.SessionProtocolVersion {
		t.Fatalf("attached protocol = %d", registry.ProtocolVersion)
	}
	client := NewClient(paths)
	if sessionID := os.Getenv("KIT_PROTOCOL_OBSERVE_SESSION"); sessionID != "" {
		snapshot, err := client.GetSessionSnapshot(ctx, sessionID)
		if err != nil || snapshot.ActiveBashExecutionID != os.Getenv("KIT_PROTOCOL_OBSERVE_BASH") {
			t.Fatalf("attach during active work: %+v, %v", snapshot, err)
		}
		return
	}
	models, err := client.ListModels(ctx)
	if err != nil || len(models.Models) == 0 {
		t.Fatalf("model catalog: %+v, %v", models, err)
	}
	workspace := os.Getenv("KIT_PROTOCOL_TEST_WORKSPACE")
	created, err := client.CreateSession(ctx, protocol.CreateSessionInput{CWD: workspace, Model: models.Models[0].ID, Name: "cross-release"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	snapshot, err := client.GetSessionSnapshot(ctx, created.ID)
	if err != nil || snapshot.Session.ID != created.ID {
		t.Fatalf("snapshot: %+v, %v", snapshot, err)
	}
	renamed, err := client.RenameSession(ctx, created.ID, "cross-release-renamed")
	if err != nil || renamed.Name != "cross-release-renamed" {
		t.Fatalf("rename: %+v, %v", renamed, err)
	}
	batch, err := client.GetSessionEvents(ctx, created.ID, snapshot.EventStreamID, snapshot.EventCursor)
	if err != nil || batch.ResyncRequired || len(batch.Events) != 1 || batch.Events[0].Kind != protocol.SessionEventSessionRenamed {
		t.Fatalf("event replay: %+v, %v", batch, err)
	}
	staleStream, err := identifier.New("stream_")
	if err != nil {
		t.Fatal(err)
	}
	resync, err := client.GetSessionEvents(ctx, created.ID, staleStream, snapshot.EventCursor)
	if err != nil || !resync.ResyncRequired || resync.StreamID != snapshot.EventStreamID {
		t.Fatalf("resynchronization: %+v, %v", resync, err)
	}
	bashID, err := identifier.New("bash_")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(workspace, "release-active-bash")
	quotedMarker := strings.ReplaceAll(marker, "'", "'\\''")
	command := "while [ ! -f '" + quotedMarker + "' ]; do sleep 0.05; done; printf protocol-ready"
	bash, err := client.StartBash(ctx, created.ID, protocol.BashExecutionInput{ExecutionID: bashID, Command: command})
	if err != nil || bash.Status != protocol.BashExecutionRunning {
		t.Fatalf("start active work: %+v, %v", bash, err)
	}
	// Release the shell even when an assertion fails before the normal signal.
	defer func() { _ = os.WriteFile(marker, []byte("ready"), 0600) }()
	active, err := client.GetSessionSnapshot(ctx, created.ID)
	if err != nil || active.ActiveBashExecutionID != bashID {
		t.Fatalf("active work snapshot: %+v, %v", active, err)
	}
	if observer := os.Getenv("KIT_PROTOCOL_OBSERVER_BIN"); observer != "" {
		cmd := exec.CommandContext(ctx, observer, "-test.run=^TestProtocolClientHelper$", "-test.v")
		cmd.Env = protocolSubprocessEnv(apphome.EnvHome+"="+paths.Home, "KIT_PROTOCOL_CLIENT_HELPER=1",
			"KIT_PROTOCOL_TEST_HOME="+paths.Home, "KIT_PROTOCOL_OBSERVE_SESSION="+created.ID,
			"KIT_PROTOCOL_OBSERVE_BASH="+bashID)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("attach while daemon has active work: %v\n%s", err, out)
		}
	}
	stream, err := client.StreamSessionEvents(ctx, created.ID, snapshot.EventStreamID, snapshot.EventCursor)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer stream.Close()
	reader := bufio.NewReader(stream)
	line, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, ":") {
		t.Fatalf("SSE greeting = %q, %v", line, err)
	}
	var gotEvent bool
	for i := 0; i < 12; i++ {
		line, err = reader.ReadString('\n')
		if err != nil {
			t.Fatalf("SSE replay: %v", err)
		}
		if strings.HasPrefix(line, "event: session.event") {
			gotEvent = true
			break
		}
	}
	if !gotEvent {
		t.Fatal("SSE did not replay a session event")
	}
	if err := os.WriteFile(marker, []byte("ready"), 0600); err != nil {
		t.Fatalf("release active shell: %v", err)
	}
	for bash.Status == protocol.BashExecutionRunning {
		bash, err = client.GetBash(ctx, created.ID, bashID)
		if err != nil {
			t.Fatalf("read active work: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if bash.Status != protocol.BashExecutionCompleted || bash.Output != "protocol-ready" {
		t.Fatalf("active work outcome: %+v", bash)
	}
	settled, err := client.GetSessionSnapshot(ctx, created.ID)
	if err != nil || len(settled.Messages) != 0 || len(settled.PendingBoundaries) != 1 || settled.PendingBoundaries[0].ID != bashID {
		t.Fatalf("protocol-40 direct-bash context boundary: %+v, %v", settled, err)
	}
	history, err := client.GetBashHistory(ctx, created.ID, 0, 10)
	if err != nil || len(history.Entries) != 1 || history.Entries[0].ID != bashID || history.Entries[0].Command != command || history.Entries[0].Status != string(protocol.BashExecutionCompleted) {
		t.Fatalf("protocol-40 bash history: %+v, %v", history, err)
	}
	listed, err := client.ListSessions(ctx, workspace)
	if err != nil || len(listed) == 0 {
		t.Fatalf("list sessions: %+v, %v", listed, err)
	}
	if err := client.DeleteSession(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = client.GetSessionSnapshot(ctx, created.ID)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Fatalf("deleted snapshot error = %v, want 404", err)
	}
	fmt.Println("protocol client contract passed")
}
