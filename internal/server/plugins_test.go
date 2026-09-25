package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/plugin"
	"github.com/akonwi/kit/internal/session"
)

func testPluginLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPluginHostFactoryUsesResolvedHomeAndCommandDomains(t *testing.T) {
	paths := apphome.FromHome(t.TempDir())
	root := filepath.Join(paths.Plugins, "reserved")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"manifestVersion":1,"id":"model","transport":{"type":"stdio","command":"./must-not-launch"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	changed := make(chan struct{}, 1)
	host := pluginHostFactory(paths, nil, testPluginLogger())(t.Context(), session.PluginSession{ID: "session-one", Name: "named", CWD: t.TempDir()}, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	defer host.Close(context.Background())
	host.Start()
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("discovery diagnostic was not published")
	}
	warnings := host.Warnings()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "reserved command domain") {
		t.Fatalf("warnings = %v", warnings)
	}
	if statuses := host.(*sessionPluginHost).Instances(); len(statuses) != 0 {
		t.Fatalf("reserved plugin launched: %#v", statuses)
	}
}

func TestPluginCommandSelectionIncludesRegistrationIdentity(t *testing.T) {
	owner := plugin.InstanceID{HostID: "pluginhost_0123456789abcdef0123456789abcdef", SessionID: "session-one", PluginID: "demo", Generation: 3}
	first := plugin.Command{ID: "demo.run", Owner: owner, Registration: 7}
	replacement := first
	replacement.Registration++
	if pluginCommandSelection(first) == pluginCommandSelection(replacement) {
		t.Fatal("same-generation command replacement retained stale selection identity")
	}
	if pluginCommandInstance(owner) == pluginCommandSelection(first) {
		t.Fatal("command selection omitted registration identity")
	}
}

func TestPluginFailureIsLoggedAndPublishedAsPersistentToast(t *testing.T) {
	var output bytes.Buffer
	host := &sessionPluginHost{logger: slog.New(slog.NewTextHandler(&output, nil))}
	toasts := make(chan session.PluginToast, 1)
	host.SetToastObserver(func(_ context.Context, toast session.PluginToast) error {
		toasts <- toast
		return nil
	})
	owner := plugin.InstanceID{HostID: "pluginhost_0123456789abcdef0123456789abcdef", SessionID: "session-one", PluginID: "demo", Generation: 3}
	host.reportFailure(plugin.FailureEvent{Owner: owner, Phase: "runtime", Message: "unexpected exit", Stderr: "failure evidence\n"})
	log := output.String()
	for _, expected := range []string{`msg="plugin failed"`, `session_id=session-one`, `plugin_id=demo`, `generation=3`, `phase=runtime`, `error="unexpected exit"`, `stderr="failure evidence\n"`} {
		if !strings.Contains(log, expected) {
			t.Fatalf("server log missing %q: %s", expected, log)
		}
	}
	toast := <-toasts
	if toast.PluginID != "demo" || toast.Title != "Plugin failed" || toast.Variant != "error" || !toast.Persistent || toast.Subtitle != "unexpected exit See the server log for details." {
		t.Fatalf("failure toast = %#v", toast)
	}
	if got := pluginFailureSubtitle(strings.Repeat("x", plugin.MaxFailureMessageBytes)); len(got) != 4096 {
		t.Fatalf("bounded failure subtitle bytes = %d", len(got))
	}
}

func TestPluginProjectContextOutsideGit(t *testing.T) {
	cwd := t.TempDir()
	host := pluginHostFactory(apphome.FromHome(t.TempDir()), nil, testPluginLogger())(t.Context(), session.PluginSession{ID: "session-one", CWD: cwd}, nil).(*sessionPluginHost)
	defer host.Close(context.Background())
	host.Start()
	projected, err := host.projectContext(t.Context(), cwd)
	if err != nil || projected.Cwd != cwd || projected.Git != nil {
		t.Fatalf("project context = %#v, %v", projected, err)
	}
}
