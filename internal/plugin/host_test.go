package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

func hostInstallation(t *testing.T, root, name, id, mode string) string {
	t.Helper()
	t.Setenv("KIT_INSTANCE_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{ManifestVersion: 1, ID: id, Transport: StdioTransport{Type: "stdio", Command: executable, Args: []string{"-test.run=^TestPluginInstanceHelper$", "--", mode}}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(writeInstallation(t, root, name, string(data)))
}

func testHost(t *testing.T, home apphome.Paths, cwd string) *Host {
	t.Helper()
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "session-one"}, ReservedIDs: []string{"model"}})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Close(ctx); err != nil {
			t.Errorf("close host: %v", err)
		}
	})
	return host
}

func eventuallyHost(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("plugin host condition did not settle")
}

func readyHost(h *Host, count int) bool {
	statuses := h.Instances()
	if len(statuses) != count {
		return false
	}
	for _, status := range statuses {
		if status.State != InstanceReady {
			return false
		}
	}
	return true
}

func TestHostLazyStartScopeChangesReloadAndClose(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	first := t.TempDir()
	second := t.TempDir()
	userRoot := hostInstallation(t, home.Plugins, "a-user", "user-plugin", "normal")
	hostInstallation(t, filepath.Join(first, ".kit", "plugins"), "project", "first-project", "normal")
	hostInstallation(t, filepath.Join(second, ".kit", "plugins"), "project", "second-project", "normal")
	host := testHost(t, home, first)
	if len(host.Instances()) != 0 {
		t.Fatal("constructed host started before runtime publication")
	}
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 2) })
	before := host.Instances()
	userGeneration := before[0].ID.Generation
	host.mu.Lock()
	oldProject := host.entries[1].instance
	host.mu.Unlock()
	host.ChangeCWD(second)
	select {
	case <-oldProject.Revoked():
	default:
		t.Fatal("project revocation was deferred")
	}
	eventuallyHost(t, func() bool {
		statuses := host.Instances()
		return readyHost(host, 2) && statuses[0].ID.Generation == userGeneration && statuses[1].ID.PluginID == "second-project"
	})
	eventuallyHost(t, func() bool {
		data, err := os.ReadFile(filepath.Join(userRoot, "project.json"))
		if err != nil {
			return false
		}
		var project ProjectContext
		return json.Unmarshal(data, &project) == nil && project.Cwd == second
	})
	host.Rename("renamed")
	eventuallyHost(t, func() bool {
		data, err := os.ReadFile(filepath.Join(userRoot, "session.json"))
		if err != nil {
			return false
		}
		var session SessionContext
		return json.Unmarshal(data, &session) == nil && session.Name != nil && *session.Name == "renamed"
	})
	host.Reload()
	eventuallyHost(t, func() bool { return readyHost(host, 2) && host.Instances()[0].ID.Generation > userGeneration })
	if err := host.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(host.Instances()) != 0 {
		t.Fatalf("instances survived close: %#v", host.Instances())
	}
	host.Start()
	if len(host.Instances()) != 0 {
		t.Fatal("closed host restarted")
	}
}

func TestHostReconcilesUserInitializationAgainstLatestContext(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	first := t.TempDir()
	second := t.TempDir()
	root := hostInstallation(t, home.Plugins, "user", "slow-user", "hold-init")
	host := testHost(t, home, first)
	host.Start()
	eventuallyHost(t, func() bool { _, err := os.Stat(filepath.Join(root, "initial.json")); return err == nil })
	host.ChangeCWD(second)
	host.Rename("latest")
	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	eventuallyHost(t, func() bool {
		data, err := os.ReadFile(filepath.Join(root, "project.json"))
		return err == nil && strings.Contains(string(data), second)
	})
	eventuallyHost(t, func() bool {
		data, err := os.ReadFile(filepath.Join(root, "session.json"))
		return err == nil && strings.Contains(string(data), "latest")
	})
	if host.Instances()[0].ID.Generation != 1 {
		t.Fatal("cwd change replaced initializing user instance")
	}
}

func TestHostFailuresDoNotBlockHealthyPluginsOrRestart(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	writeInstallation(t, home.Plugins, "a-invalid", "{}")
	writeInstallation(t, home.Plugins, "b-launch", `{"manifestVersion":1,"id":"missing","transport":{"type":"stdio","command":"./missing"}}`)
	hostInstallation(t, home.Plugins, "c-reject", "reject", "bad-version")
	goodRoot := hostInstallation(t, home.Plugins, "d-good", "good", "normal")
	hostInstallation(t, home.Plugins, "e-reserved", "model", "normal")
	host := testHost(t, home, cwd)
	host.Start()
	eventuallyHost(t, func() bool {
		statuses := host.Instances()
		return len(statuses) == 3 && statuses[0].State == InstanceFailed && statuses[1].State == InstanceFailed && statuses[2].State == InstanceReady
	})
	before := host.Instances()
	if warnings := host.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "missing") || !strings.Contains(warnings[0], "reserved") {
		t.Fatalf("warnings = %v", warnings)
	}
	host.Rename("after-failure")
	eventuallyHost(t, func() bool {
		data, err := os.ReadFile(filepath.Join(goodRoot, "session.json"))
		return err == nil && strings.Contains(string(data), "after-failure")
	})
	after := host.Instances()
	for i := range before {
		if after[i].ID != before[i].ID {
			t.Fatal("failed instance was automatically replaced")
		}
	}
}

func TestHostReportsCompletedFailureOnceWithStderr(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	hostInstallation(t, home.Plugins, "user", "failed-plugin", "normal")
	failures := make(chan FailureEvent, 2)
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "session-one"}, Failure: func(failure FailureEvent) {
		failures <- failure
	}})
	defer host.Close(context.Background())
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	host.mu.Lock()
	instance := host.entries[0].instance
	host.mu.Unlock()
	_, _ = instance.Call(t.Context(), "exit", nil)
	var failure FailureEvent
	select {
	case failure = <-failures:
	case <-time.After(5 * time.Second):
		t.Fatal("completed failure was not reported")
	}
	if failure.Owner.PluginID != "failed-plugin" || failure.Phase != "runtime" || failure.Message == "" || failure.Stderr != "unexpected exit diagnostic\n" {
		t.Fatalf("failure = %#v", failure)
	}
	time.Sleep(300 * time.Millisecond)
	select {
	case duplicate := <-failures:
		t.Fatalf("duplicate failure = %#v", duplicate)
	default:
	}
}

func TestHostCleanReloadDoesNotReportFailure(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	hostInstallation(t, home.Plugins, "user", "healthy", "normal")
	failures := make(chan FailureEvent, 1)
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: t.TempDir(), Session: SessionContext{ID: "session-one"}, Failure: func(failure FailureEvent) {
		failures <- failure
	}})
	defer host.Close(context.Background())
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	generation := host.Instances()[0].ID.Generation
	host.Reload()
	eventuallyHost(t, func() bool { return readyHost(host, 1) && host.Instances()[0].ID.Generation > generation })
	select {
	case failure := <-failures:
		t.Fatalf("clean reload failure = %#v", failure)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestHostReportsLaunchFailureWithoutStderr(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	writeInstallation(t, home.Plugins, "missing", `{"manifestVersion":1,"id":"missing","transport":{"type":"stdio","command":"./missing"}}`)
	failures := make(chan FailureEvent, 1)
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: t.TempDir(), Session: SessionContext{ID: "session-one"}, Failure: func(failure FailureEvent) {
		failures <- failure
	}})
	defer host.Close(context.Background())
	host.Start()
	select {
	case failure := <-failures:
		if failure.Owner.PluginID != "missing" || failure.Phase != "launch" || failure.Message == "" || failure.Stderr != "" {
			t.Fatalf("launch failure = %#v", failure)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("launch failure was not reported")
	}
}

func TestHostCloseBeforeStartAndIsolatedSessionOwnership(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	hostInstallation(t, home.Plugins, "user", "shared-installation", "normal")
	closed := testHost(t, home, cwd)
	if err := closed.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	closed.Start()
	if len(closed.Instances()) != 0 {
		t.Fatal("close-before-start launched a plugin")
	}
	first := testHost(t, home, cwd)
	second := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "session-two"}})
	defer second.Close(context.Background())
	first.Start()
	second.Start()
	eventuallyHost(t, func() bool { return readyHost(first, 1) && readyHost(second, 1) })
	if first.Instances()[0].ID.SessionID == second.Instances()[0].ID.SessionID {
		t.Fatal("shared session identity")
	}
	if err := first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !readyHost(second, 1) {
		t.Fatal("closing one session stopped another")
	}
}

func TestHostProjectProbeFailureStillPublishesAuthoritativeCWD(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	first := t.TempDir()
	second := t.TempDir()
	root := hostInstallation(t, home.Plugins, "user", "user", "normal")
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: first, Session: SessionContext{ID: "one"}, Project: func(_ context.Context, cwd string) (ProjectContext, error) {
		if cwd == second {
			return ProjectContext{Cwd: cwd}, errors.New("Git probe failed")
		}
		return ProjectContext{Cwd: cwd}, nil
	}})
	defer host.Close(context.Background())
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	host.ChangeCWD(second)
	eventuallyHost(t, func() bool {
		data, err := os.ReadFile(filepath.Join(root, "project.json"))
		if err != nil {
			return false
		}
		var project ProjectContext
		return json.Unmarshal(data, &project) == nil && project.Cwd == second && project.Git == nil
	})
	if warnings := host.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "Git probe failed") {
		t.Fatalf("probe diagnostic = %v", warnings)
	}
}

func TestHostReloadRetainsCleanupFailureDiagnostics(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	hostInstallation(t, home.Plugins, "user", "bad-shutdown", "bad-shutdown")
	failures := make(chan FailureEvent, 1)
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "one"}, Failure: func(failure FailureEvent) {
		failures <- failure
	}})
	defer host.Close(context.Background())
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	generation := host.Instances()[0].ID.Generation
	host.Reload()
	eventuallyHost(t, func() bool { return readyHost(host, 1) && host.Instances()[0].ID.Generation > generation })
	if warnings := host.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "cleanup") {
		t.Fatalf("cleanup diagnostic disappeared: %v", warnings)
	}
	select {
	case failure := <-failures:
		if failure.Owner.PluginID != "bad-shutdown" || failure.Phase != "shutdown" {
			t.Fatalf("shutdown failure = %#v", failure)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown failure was not reported")
	}
}

func TestHostRejectsStaleHandlerMutationAfterReload(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	hostInstallation(t, home.Plugins, "user", "user", "normal")
	host := testHost(t, home, cwd)
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	owner := host.Instances()[0].ID
	host.Reload()
	_, err := host.unsupportedRequest(t.Context(), owner, "old-generation-method")
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32002 {
		t.Fatalf("stale mutation = %v", err)
	}
	for _, warning := range host.Warnings() {
		if strings.Contains(warning, "old-generation-method") {
			t.Fatal("stale handler published diagnostic")
		}
	}
}

func TestHostRecreatedRuntimeDoesNotReuseOwnerIdentity(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	hostInstallation(t, home.Plugins, "user", "user", "normal")
	first := testHost(t, home, cwd)
	first.Start()
	eventuallyHost(t, func() bool { return readyHost(first, 1) })
	previous := first.Instances()[0].ID
	if err := first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := testHost(t, home, cwd)
	next.Start()
	eventuallyHost(t, func() bool { return readyHost(next, 1) })
	current := next.Instances()[0].ID
	if previous.SessionID != current.SessionID || previous.PluginID != current.PluginID || previous.HostID == current.HostID {
		t.Fatalf("runtime owner identity = %#v -> %#v", previous, current)
	}
	_, err := next.unsupportedRequest(t.Context(), previous, "stale-runtime")
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32002 {
		t.Fatalf("previous runtime owner accepted: %v", err)
	}
}
