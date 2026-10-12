package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/version"
	"github.com/gofrs/flock"
)

// TestSubprocessReleaseGate builds two Kit binaries from the current source
// with different release labels. Because compatibility depends only on the
// session protocol, each label's client attaches to the daemon of either label
// without disrupting it. This is not evidence of compatibility across source
// revisions.
func TestSubprocessReleaseGate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess builds in short mode")
	}
	runReleaseGate(t, protocolTestRoot(t), [2]string{"release-a", "release-b"})
}

// protocolSubprocessEnv prevents ambient helper controls from leaking
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

// runReleaseGate builds a daemon and a protocol client for each label from the
// source at root, then runs every client against every daemon. While each
// client holds active work, the other label's client attaches to observe it.
func runReleaseGate(t *testing.T, root string, labels [2]string) {
	t.Helper()
	binDir := t.TempDir()
	for _, label := range labels {
		buildKitBinary(t, root, filepath.Join(binDir, "kit-"+label), label)
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		cmd := exec.CommandContext(ctx, "go", "test", "-c", "-ldflags", "-X github.com/akonwi/kit/internal/version.Version="+label,
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
			if err != nil || before.KitVersion != daemonLabel || before.ProtocolVersion != version.SessionProtocolVersion {
				t.Fatalf("daemon registry = %+v, %v", before, err)
			}
			other := labels[1-index]
			for _, clientLabel := range []string{daemonLabel, other} {
				client := filepath.Join(binDir, "client-"+clientLabel)
				observerLabel := daemonLabel
				if clientLabel == daemonLabel {
					observerLabel = other
				}
				clientEnv := append(append([]string{}, env...), "KIT_PROTOCOL_CLIENT_HELPER=1",
					"KIT_PROTOCOL_TEST_HOME="+paths.Home, "KIT_PROTOCOL_TEST_WORKSPACE="+workspace,
					"KIT_PROTOCOL_OBSERVER_BIN="+filepath.Join(binDir, "client-"+observerLabel))
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
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if registry.ProtocolVersion != version.SessionProtocolVersion {
		t.Fatalf("attached protocol = %d", registry.ProtocolVersion)
	}
	client := NewClient(paths)
	if sessionID := os.Getenv("KIT_PROTOCOL_OBSERVE_SESSION"); sessionID != "" {
		snapshot, err := client.transport.GetSessionSnapshot(ctx, sessionID)
		if err != nil || snapshot.ActiveBashExecutionID != os.Getenv("KIT_PROTOCOL_OBSERVE_BASH") {
			t.Fatalf("attach during active work: %+v, %v", snapshot, err)
		}
		return
	}
	models, err := client.transport.ListModels(ctx)
	if err != nil || len(models.Models) == 0 {
		t.Fatalf("model catalog: %+v, %v", models, err)
	}
	workspace := os.Getenv("KIT_PROTOCOL_TEST_WORKSPACE")
	created, err := client.transport.CreateSession(ctx, protocol.CreateSessionInput{CWD: workspace, Model: models.Models[0].ID, Name: "cross-release"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	snapshot, err := client.transport.GetSessionSnapshot(ctx, created.ID)
	if err != nil || snapshot.Session.ID != created.ID {
		t.Fatalf("snapshot: %+v, %v", snapshot, err)
	}
	renamed, err := client.transport.RenameSession(ctx, created.ID, "cross-release-renamed")
	if err != nil || renamed.Name != "cross-release-renamed" {
		t.Fatalf("rename: %+v, %v", renamed, err)
	}
	batch, err := client.transport.GetSessionEvents(ctx, created.ID, snapshot.EventStreamID, snapshot.EventCursor)
	if err != nil || batch.ResyncRequired || len(batch.Events) != 1 || batch.Events[0].Kind() != protocol.SessionEventSessionNameChanged {
		t.Fatalf("event replay: %+v, %v", batch, err)
	}
	staleStream, err := identifier.New("stream_")
	if err != nil {
		t.Fatal(err)
	}
	resync, err := client.transport.GetSessionEvents(ctx, created.ID, staleStream, snapshot.EventCursor)
	if err != nil || !resync.ResyncRequired || resync.StreamID != snapshot.EventStreamID {
		t.Fatalf("resynchronization: %+v, %v", resync, err)
	}
	bashID, err := identifier.New("bash_")
	if err != nil {
		t.Fatal(err)
	}
	// Each client run in the shared workspace waits on its own marker.
	marker := filepath.Join(workspace, "release-active-"+bashID)
	quotedMarker := strings.ReplaceAll(marker, "'", "'\\''")
	command := "while [ ! -f '" + quotedMarker + "' ]; do sleep 0.05; done; printf protocol-ready"
	bash, err := client.transport.StartBash(ctx, created.ID, protocol.BashExecutionInput{ExecutionID: bashID, Command: command})
	if err != nil || bash.Status != protocol.BashExecutionRunning {
		t.Fatalf("start active work: %+v, %v", bash, err)
	}
	// Release the shell even when an assertion fails before the normal signal.
	defer func() { _ = os.WriteFile(marker, []byte("ready"), 0600) }()
	active, err := client.transport.GetSessionSnapshot(ctx, created.ID)
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
	stream, err := client.transport.StreamSessionEvents(ctx, created.ID, snapshot.EventStreamID, snapshot.EventCursor)
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
		bash, err = client.transport.GetBash(ctx, created.ID, bashID)
		if err != nil {
			t.Fatalf("read active work: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if bash.Status != protocol.BashExecutionCompleted || bash.Output != "protocol-ready" {
		t.Fatalf("active work outcome: %+v", bash)
	}
	settled, err := client.transport.GetSessionSnapshot(ctx, created.ID)
	if err != nil || len(settled.Messages) != 0 || len(settled.PendingBoundaries) != 1 || settled.PendingBoundaries[0].ID != bashID {
		t.Fatalf("protocol-42 direct-bash context boundary: %+v, %v", settled, err)
	}
	history, err := client.transport.GetBashHistory(ctx, created.ID, 0, 10)
	if err != nil || len(history.Entries) != 1 || history.Entries[0].ID != bashID || history.Entries[0].Command != command || history.Entries[0].Status != string(protocol.BashExecutionCompleted) {
		t.Fatalf("protocol-42 bash history: %+v, %v", history, err)
	}
	listed, err := client.transport.ListSessions(ctx, workspace)
	if err != nil || len(listed) == 0 {
		t.Fatalf("list sessions: %+v, %v", listed, err)
	}
	if err := client.transport.DeleteSession(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = client.transport.GetSessionSnapshot(ctx, created.ID)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Fatalf("deleted snapshot error = %v, want 404", err)
	}
	fmt.Println("protocol client contract passed")
}
