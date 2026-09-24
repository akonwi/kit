package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

func commandHost(t *testing.T) (*Host, string) {
	t.Helper()
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	root := hostInstallation(t, home.Plugins, "commands", "demo", "commands")
	host := testHost(t, home, cwd)
	host.Start()
	eventuallyHost(t, func() bool { return len(host.Commands()) == 4 })
	return host, root
}

func findCommand(t *testing.T, host *Host, id string) Command {
	t.Helper()
	for _, command := range host.Commands() {
		if command.ID == id {
			return command
		}
	}
	t.Fatalf("command %s not found: %#v", id, host.Commands())
	return Command{}
}

func TestCommandRegistrationOwnershipAndExecution(t *testing.T) {
	host, root := commandHost(t)
	commands := host.Commands()
	var ids []string
	for _, command := range commands {
		ids = append(ids, command.ID)
	}
	if !reflect.DeepEqual(ids, []string{"demo.fail", "demo.invalid", "demo.ok", "demo.wait"}) {
		t.Fatalf("commands = %v", ids)
	}
	command := findCommand(t, host, "demo.ok")
	if command.LocalID != "ok" || command.Description != "Run ok" || command.ArgName != "value" || command.Owner.SessionID != "session-one" {
		t.Fatalf("command = %#v", command)
	}
	args := "literal $(echo untouched)\nsecond line"
	if err := host.ExecuteCommand(t.Context(), command, args); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "command.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ID   string `json:"id"`
		Args string `json:"args"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "ok" || got.Args != args {
		t.Fatalf("execution = %#v", got)
	}
	owner := command.Owner
	if _, err := host.handleRequest(t.Context(), owner, "kit/commands/register", json.RawMessage(`{"id":"ok","description":"duplicate"}`)); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	for range 2 {
		result, err := host.handleRequest(t.Context(), owner, "kit/commands/unregister", json.RawMessage(`{"id":"ok"}`))
		if err != nil || string(result) != "null" {
			t.Fatalf("unregister = %s, %v", result, err)
		}
	}
	if err := host.ExecuteCommand(t.Context(), command, ""); !errors.Is(err, ErrCommandUnavailable) {
		t.Fatalf("removed command = %v", err)
	}
	if len(host.Commands()) != 3 {
		t.Fatalf("remaining commands = %#v", host.Commands())
	}
}

func TestCommandReregistrationRejectsStaleSelection(t *testing.T) {
	host, _ := commandHost(t)
	stale := findCommand(t, host, "demo.ok")
	if _, err := host.handleRequest(t.Context(), stale.Owner, "kit/commands/unregister", json.RawMessage(`{"id":"ok"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := host.handleRequest(t.Context(), stale.Owner, "kit/commands/register", json.RawMessage(`{"id":"ok","description":"Run replacement","argName":"value"}`)); err != nil {
		t.Fatal(err)
	}
	replacement := findCommand(t, host, "demo.ok")
	if replacement.Registration == stale.Registration {
		t.Fatalf("replacement retained registration %d", stale.Registration)
	}
	if err := host.ExecuteCommand(t.Context(), stale, ""); !errors.Is(err, ErrCommandUnavailable) {
		t.Fatalf("stale selection executed replacement: %v", err)
	}
	if err := host.ExecuteCommand(t.Context(), replacement, ""); err != nil {
		t.Fatalf("replacement command failed: %v", err)
	}
}

func TestCommandFailureCancellationAndMalformedResult(t *testing.T) {
	host, root := commandHost(t)
	failure := findCommand(t, host, "demo.fail")
	var rpcFailure *RPCError
	if err := host.ExecuteCommand(t.Context(), failure, ""); !errors.As(err, &rpcFailure) || rpcFailure.Code != -32000 {
		t.Fatalf("remote failure = %v", err)
	}
	if len(host.Commands()) != 4 {
		t.Fatal("ordinary command failure revoked healthy plugin")
	}
	waiting := findCommand(t, host, "demo.wait")
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- host.ExecuteCommand(ctx, waiting, "") }()
	eventuallyHost(t, func() bool {
		data, err := os.ReadFile(filepath.Join(root, "command.json"))
		return err == nil && strings.Contains(string(data), "wait")
	})
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	malformed := findCommand(t, host, "demo.invalid")
	if err := host.ExecuteCommand(t.Context(), malformed, ""); err == nil {
		t.Fatal("non-null command result accepted")
	}
	eventuallyHost(t, func() bool { return len(host.Commands()) == 0 })
}

func TestCommandCatalogAndCallsCannotCrossReload(t *testing.T) {
	host, root := commandHost(t)
	command := findCommand(t, host, "demo.wait")
	result := make(chan error, 1)
	go func() { result <- host.ExecuteCommand(t.Context(), command, "") }()
	eventuallyHost(t, func() bool {
		data, err := os.ReadFile(filepath.Join(root, "command.json"))
		return err == nil && strings.Contains(string(data), "wait")
	})
	host.Reload()
	if len(host.Commands()) != 0 {
		t.Fatal("reload did not atomically revoke command catalog")
	}
	if err := <-result; err == nil {
		t.Fatal("revoked command succeeded")
	}
	eventuallyHost(t, func() bool {
		commands := host.Commands()
		return len(commands) == 4 && commands[0].Owner != command.Owner
	})
	if err := host.ExecuteCommand(t.Context(), command, ""); !errors.Is(err, ErrCommandUnavailable) {
		t.Fatalf("old owner replayed on replacement: %v", err)
	}
	if _, err := host.handleRequest(t.Context(), command.Owner, "kit/commands/register", json.RawMessage(`{"id":"late","description":"Late"}`)); err == nil {
		t.Fatal("old generation registered a command")
	}
	next := findCommand(t, host, "demo.ok")
	if err := host.ExecuteCommand(t.Context(), next, ""); err != nil {
		t.Fatal(err)
	}
}

func TestCommandChangesPublishWhileAnotherPluginInitializes(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	hostInstallation(t, home.Plugins, "a-fast", "fast", "commands")
	slowRoot := hostInstallation(t, home.Plugins, "z-slow", "slow", "hold-init")
	changed := make(chan struct{}, 1)
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "one"}, Changed: func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}})
	defer host.Close(context.Background())
	host.Start()
	eventuallyHost(t, func() bool {
		_, err := os.Stat(filepath.Join(slowRoot, "initial.json"))
		return err == nil && len(host.Commands()) == 4
	})
	for {
		select {
		case <-changed:
			continue
		default:
			goto drained
		}
	}
drained:
	command := findCommand(t, host, "fast.ok")
	if _, err := host.handleRequest(t.Context(), command.Owner, "kit/commands/unregister", json.RawMessage(`{"id":"ok"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("catalog update waited behind initialization")
	}
	statuses := host.Instances()
	if len(statuses) != 2 || statuses[1].State != InstanceInitializing {
		t.Fatalf("slow initialization = %#v", statuses)
	}
}

func TestCommandAndToastPayloadValidation(t *testing.T) {
	valid := `{"id":"settings.open","description":"Open settings","argName":null,"category":"Example"}`
	if command, err := parseCommand(json.RawMessage(valid)); err != nil || command.LocalID != "settings.open" {
		t.Fatalf("valid command = %#v, %v", command, err)
	}
	for _, raw := range []string{`null`, `[]`, `{"id":"ok"}`, `{"id":"Bad","description":"x"}`, `{"id":"ok","description":null}`, `{"id":"ok","description":" "}`, `{"ID":"ok","description":"x"}`, `{"id":"ok","description":"x","extra":true}`, `{"id":"ok","description":"\u001b[31m"}`, `{"id":"ok","description":"x","argName":3}`} {
		if _, err := parseCommand(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted invalid command %s", raw)
		}
	}
	for _, raw := range []string{`{"title":"Notice","variant":"info","subtitle":"one\ntwo","persistent":true}`, `{"title":"Notice","variant":"warning","subtitle":null}`} {
		if _, err := parseToast(json.RawMessage(raw)); err != nil {
			t.Errorf("valid toast: %v", err)
		}
	}
	for _, raw := range []string{`{"title":"Notice","variant":"success"}`, `{"title":"Notice","variant":"info","persistent":null}`, `{"title":"Notice","variant":"info","subtitle":"\u001b"}`} {
		if _, err := parseToast(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted invalid toast %s", raw)
		}
	}
}

func TestDemoV1PluginCommandCompatibility(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("v1 Python fixture requires python3")
	}
	home := apphome.FromHome(t.TempDir())
	root := filepath.Join(home.Plugins, "plugin-demo")
	cwd := t.TempDir()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plugin.py", "plugin.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", ".kit", "plugins", "plugin-demo", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	notices := make(chan Toast, 8)
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "python-session"}, Toast: func(ctx context.Context, toast Toast) error {
		select {
		case notices <- toast:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	defer host.Close(context.Background())
	host.Start()
	eventuallyHost(t, func() bool { return len(host.Commands()) == 2 })
	echo := findCommand(t, host, "plugin-demo.echo")
	inspect := findCommand(t, host, "plugin-demo.context")
	if err := host.ExecuteCommand(t.Context(), echo, "literal demo arguments"); err != nil {
		t.Fatal(err)
	}
	select {
	case toast := <-notices:
		if toast.Title != "Plugin echo" || toast.Subtitle != "literal demo arguments" || toast.Variant != "info" || toast.Owner != echo.Owner {
			t.Fatalf("toast = %#v", toast)
		}
	case <-time.After(time.Second):
		t.Fatal("missing echo toast")
	}
	second := t.TempDir()
	host.ChangeCWD(second)
	host.Rename("Renamed")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := host.ExecuteCommand(ctx, inspect, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case toast := <-notices:
		if toast.Title != "Plugin context" || toast.Subtitle != "Renamed\n"+second || toast.Owner != inspect.Owner {
			t.Fatalf("context-reconciled toast = %#v", toast)
		}
	case <-ctx.Done():
		t.Fatal("missing context toast")
	}
	if warnings := host.Warnings(); len(warnings) != 0 {
		t.Fatalf("fixture compatibility warnings = %v", warnings)
	}
}

func TestCommandRegistryLimitAndArgumentValidation(t *testing.T) {
	host, _ := commandHost(t)
	command := findCommand(t, host, "demo.ok")
	for n := 4; n < MaxCommands; n++ {
		raw, _ := json.Marshal(map[string]string{"id": fmt.Sprintf("extra-%d", n), "description": "Extra"})
		if _, err := host.handleRequest(t.Context(), command.Owner, "kit/commands/register", raw); err != nil {
			t.Fatal(err)
		}
	}
	if len(host.Commands()) != MaxCommands {
		t.Fatalf("catalog count = %d", len(host.Commands()))
	}
	var failure *RPCError
	if _, err := host.handleRequest(t.Context(), command.Owner, "kit/commands/register", json.RawMessage(`{"id":"overflow","description":"Extra"}`)); !errors.As(err, &failure) || failure.Code != -32005 {
		t.Fatalf("limit error = %v", err)
	}
	for _, args := range []string{strings.Repeat("a", MaxCommandArgsBytes+1), "with\x00nul", string([]byte{255})} {
		if err := host.ExecuteCommand(t.Context(), command, args); !errors.As(err, &failure) || failure.Code != -32602 {
			t.Fatalf("argument validation = %v", err)
		}
	}
}

func TestContributionObjectBoundsAndDuplicateFields(t *testing.T) {
	for _, raw := range []string{
		`{"id":"ok","id":"replacement","description":"x"}`,
		`{"id":"ok","description":"x"} {}`,
		`{"id":"ok","description":"x",` + strings.Repeat(`"unknown":0,`, 1000) + `"tail":0}`,
		`{"id":"ok","description":"` + strings.Repeat("x", MaxContributionParamsBytes) + `"}`,
	} {
		if _, err := parseCommand(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted invalid payload of %d bytes", len(raw))
		}
	}
	if _, err := parseToast(json.RawMessage(`{"title":"Notice","variant":"info","variant":"error"}`)); err == nil {
		t.Fatal("duplicate toast key accepted")
	}
	if _, err := parseLocalID(json.RawMessage(`{"id":"a","id":"b"}`)); err == nil {
		t.Fatal("duplicate unregister key accepted")
	}
}
