package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

func gitEventHost(t *testing.T, cwd string, project func(context.Context, string) (ProjectContext, error)) (*Host, string) {
	t.Helper()
	home := apphome.FromHome(t.TempDir())
	root := hostInstallation(t, home.Plugins, "git", "git-observer", "normal")
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "session-one"}, Project: project})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := host.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	return host, root
}
func gitEvents(t *testing.T, h *Host, root string) []string {
	t.Helper()
	h.mu.Lock()
	var instance *Instance
	for _, entry := range h.entries {
		if entry.installation.Root == root {
			instance = entry.instance
			break
		}
	}
	h.mu.Unlock()
	if _, err := instance.Call(t.Context(), "echo", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "git-events.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var event rpcMessage
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.ID != nil || event.Method == nil || *event.Method != "kit/events/git.changed" {
			t.Fatalf("bad Git notification: %s", line)
		}
		result = append(result, string(event.Params))
	}
	return result
}
func TestGitEventsTrackPublicProjectionWithoutInitialReplayOrDuplicates(t *testing.T) {
	cwd := t.TempDir()
	branch := "main"
	var mu sync.Mutex
	state := &GitContext{Root: cwd, Branch: &branch}
	h, root := gitEventHost(t, cwd, func(_ context.Context, cwd string) (ProjectContext, error) {
		mu.Lock()
		defer mu.Unlock()
		return ProjectContext{Cwd: cwd, Git: cloneGitContext(state)}, nil
	})
	h.refreshGit()
	if events := gitEvents(t, h, root); len(events) != 0 {
		t.Fatalf("initial Git replay=%v", events)
	}
	for index, next := range []*GitContext{{Root: cwd, Branch: &branch, Dirty: true}, {Root: cwd, Branch: nil, Dirty: true}, {Root: filepath.Join(cwd, "nested"), Branch: &branch}, nil, {Root: cwd, Branch: &branch}} {
		mu.Lock()
		state = next
		mu.Unlock()
		h.refreshGit()
		h.refreshGit()
		events := gitEvents(t, h, root)
		expected, _ := json.Marshal(struct {
			Git *GitContext `json:"git"`
		}{next})
		if len(events) != index+1 || events[index] != string(expected) {
			t.Fatalf("Git events=%v want last=%s", events, expected)
		}
	}
}
func TestGitProbeCannotPublishAcrossCWDChange(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	branch := "main"
	var hold atomic.Bool
	started, release := make(chan struct{}), make(chan struct{})
	h, root := gitEventHost(t, first, func(ctx context.Context, cwd string) (ProjectContext, error) {
		if cwd != first {
			return ProjectContext{Cwd: cwd}, nil
		}
		git := &GitContext{Root: first, Branch: &branch}
		if hold.Swap(false) {
			git.Dirty = true
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ProjectContext{}, ctx.Err()
			}
		}
		return ProjectContext{Cwd: cwd, Git: git}, nil
	})
	hold.Store(true)
	done := make(chan struct{})
	go func() { h.refreshGit(); close(done) }()
	<-started
	h.ChangeCWD(second)
	eventuallyHost(t, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.entries[0].context.Project.Cwd == second })
	close(release)
	<-done
	events := gitEvents(t, h, root)
	if len(events) != 1 || events[0] != `{"git":null}` {
		t.Fatalf("stale sample crossed cwd: %v", events)
	}
	raw, err := os.ReadFile(filepath.Join(root, "context-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("context notifications=%s", raw)
	}
	for index, method := range []string{"kit/events/project.changed", "kit/events/git.changed"} {
		var message rpcMessage
		if err := json.Unmarshal([]byte(lines[index]), &message); err != nil {
			t.Fatal(err)
		}
		if message.Method == nil || *message.Method != method {
			t.Fatalf("context notification order=%s", raw)
		}
	}
}
func TestGitProbeCancellationIsJoinedByHostClose(t *testing.T) {
	var hold atomic.Bool
	started, ended := make(chan struct{}), make(chan struct{})
	h, _ := gitEventHost(t, t.TempDir(), func(ctx context.Context, cwd string) (ProjectContext, error) {
		if hold.Load() {
			close(started)
			<-ctx.Done()
			close(ended)
			return ProjectContext{}, ctx.Err()
		}
		return ProjectContext{Cwd: cwd}, nil
	})
	hold.Store(true)
	select {
	case <-started:
	case <-time.After(gitEventPollInterval + 2*time.Second):
		t.Fatal("Git poll did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	default:
		t.Fatal("host closed before Git probe exited")
	}
}

func TestGitProbeDoesNotRetargetReplacementGeneration(t *testing.T) {
	cwd := t.TempDir()
	branch := "main"
	var hold atomic.Bool
	started, release := make(chan struct{}), make(chan struct{})
	h, root := gitEventHost(t, cwd, func(ctx context.Context, cwd string) (ProjectContext, error) {
		git := &GitContext{Root: cwd, Branch: &branch}
		if hold.Swap(false) {
			git.Dirty = true
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ProjectContext{}, ctx.Err()
			}
		}
		return ProjectContext{Cwd: cwd, Git: git}, nil
	})
	old := h.Instances()[0].ID
	hold.Store(true)
	done := make(chan struct{})
	go func() { h.refreshGit(); close(done) }()
	<-started
	h.Reload()
	eventuallyHost(t, func() bool { return readyHost(h, 1) && h.Instances()[0].ID != old })
	close(release)
	<-done
	if events := gitEvents(t, h, root); len(events) != 0 {
		t.Fatalf("replacement inherited Git sample: %v", events)
	}
}
func TestGitProbeExcludesLateReadyGenerationAndDoesNotWaitForInitialization(t *testing.T) {
	cwd := t.TempDir()
	branch := "main"
	var hold, dirty atomic.Bool
	started, release := make(chan struct{}), make(chan struct{})
	h, root := gitEventHost(t, cwd, func(ctx context.Context, cwd string) (ProjectContext, error) {
		git := &GitContext{Root: cwd, Branch: &branch, Dirty: dirty.Load()}
		if hold.Swap(false) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ProjectContext{}, ctx.Err()
			}
		}
		return ProjectContext{Cwd: cwd, Git: git}, nil
	})
	late := hostInstallation(t, h.config.Home.Plugins, "z-late", "late", "hold-init")
	h.Reload()
	eventuallyHost(t, func() bool {
		statuses := h.Instances()
		return len(statuses) == 2 && statuses[0].State == InstanceReady && statuses[1].State == InstanceInitializing
	})
	// Initialization can stall, but a ready plugin still receives live changes.
	dirty.Store(true)
	h.refreshGit()
	if events := gitEvents(t, h, root); len(events) != 1 {
		t.Fatalf("slow initialization blocked observation: %v", events)
	}
	hold.Store(true)
	done := make(chan struct{})
	go func() { h.refreshGit(); close(done) }()
	<-started
	if err := os.WriteFile(filepath.Join(late, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	eventuallyHost(t, func() bool { return readyHost(h, 2) })
	close(release)
	<-done
	if events := gitEvents(t, h, late); len(events) != 0 {
		t.Fatalf("late generation inherited sample: %v", events)
	}
	dirty.Store(true)
	h.refreshGit()
	if events := gitEvents(t, h, late); len(events) != 1 {
		t.Fatalf("late generation missed next observation: %v", events)
	}
}

func TestGitContextRoundTripDiscardsStaleProjectEvent(t *testing.T) {
	instance, root := testInstance(t, t.Context(), "normal", RPCHandlers{}, testInstanceDeadlines())
	eventuallyHost(t, func() bool { return instance.Status().State == InstanceReady })
	first, second := t.TempDir(), t.TempDir()
	started, release := make(chan struct{}), make(chan struct{})
	h := NewHost(t.Context(), HostConfig{CWD: first, Session: SessionContext{ID: "session-one"}, Project: func(ctx context.Context, cwd string) (ProjectContext, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return ProjectContext{}, ctx.Err()
		}
		return ProjectContext{Cwd: cwd}, nil
	}})
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	entry := &hostEntry{installation: Installation{Root: root, Source: User}, instance: instance, owner: instance.Status().ID, epoch: 1, projectEpoch: 1, context: PluginContext{Project: ProjectContext{Cwd: first}, Session: SessionContext{ID: "session-one"}}}
	h.entries = []*hostEntry{entry}
	h.ChangeCWD(second)
	done := make(chan struct{})
	go func() { h.reconcileContext(entry); close(done) }()
	<-started
	h.ChangeCWD(first)
	close(release)
	<-done
	h.reconcileContext(entry)
	_ = gitEvents(t, h, root) // Pipe barrier proves any admitted notification was processed.
	if raw, err := os.ReadFile(filepath.Join(root, "context-events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("A -> B -> A emitted stale context: %s, %v", raw, err)
	}
	if entry.context.Project.Cwd != first {
		t.Fatalf("context baseline=%#v", entry.context.Project)
	}
}

func TestGitEventsIncludePRChangesAndCloneInitializationContext(t *testing.T) {
	cwd := t.TempDir()
	branch := "main"
	var mu sync.Mutex
	state := &GitContext{Root: cwd, Branch: &branch, PullRequest: &PullRequestContext{Number: 12, URL: "https://github.com/a/b/pull/12"}}
	h, root := gitEventHost(t, cwd, func(_ context.Context, cwd string) (ProjectContext, error) {
		mu.Lock()
		defer mu.Unlock()
		return ProjectContext{Cwd: cwd, Git: cloneGitContext(state)}, nil
	})
	raw, err := os.ReadFile(filepath.Join(root, "initial.json"))
	if err != nil {
		t.Fatal(err)
	}
	var initial struct {
		Context PluginContext `json:"context"`
	}
	if err := json.Unmarshal(raw, &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Context.Project.Git == nil || initial.Context.Project.Git.PullRequest == nil || initial.Context.Project.Git.PullRequest.Number != 12 {
		t.Fatalf("initial PR context=%s", raw)
	}
	for index, pr := range []*PullRequestContext{{Number: 13, URL: "https://github.com/a/b/pull/13"}, nil} {
		mu.Lock()
		state.PullRequest = pr
		mu.Unlock()
		h.refreshGit()
		h.refreshGit()
		events := gitEvents(t, h, root)
		expected, _ := json.Marshal(struct {
			Git *GitContext `json:"git"`
		}{state})
		if len(events) != index+1 || events[index] != string(expected) {
			t.Fatalf("PR-only events=%v want %s", events, expected)
		}
	}
	original := &GitContext{Root: cwd, PullRequest: &PullRequestContext{Number: 42}}
	copy := cloneGitContext(original)
	copy.PullRequest.Number = 99
	if original.PullRequest.Number != 42 {
		t.Fatal("Git context clone aliases PR metadata")
	}
}
