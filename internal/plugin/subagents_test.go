package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

func registerSubagent(t *testing.T, host *Host, owner InstanceID, params string) string {
	t.Helper()
	result, err := host.handleRequest(t.Context(), owner, "kit/subagents/register", json.RawMessage(params))
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(result, &response) != nil {
		t.Fatalf("registration result = %s", result)
	}
	return response.ID
}

func TestSubprocessPluginRegistersSubagent(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	root := hostInstallation(t, home.Plugins, "demo", "demo", "subagents")
	host := testHost(t, home, t.TempDir())
	host.Start()
	eventuallyHost(t, func() bool {
		_, err := os.Stat(filepath.Join(root, "subagent-result.json"))
		return len(host.Subagents()) == 1 && err == nil
	})
	result, err := os.ReadFile(filepath.Join(root, "subagent-result.json"))
	if err != nil || string(result) != `{"id":"demo.reviewer"}` {
		t.Fatalf("registration response = %q, %v", result, err)
	}
}

func TestHostRegistersAndUnregistersGenerationOwnedSubagents(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	manifestRoot := hostInstallation(t, home.Plugins, "demo", "demo", "normal")
	host := testHost(t, home, cwd)
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	owner := host.Instances()[0].ID
	if id := registerSubagent(t, host, owner, `{"id":"reviewer","description":"Review changes","instructions":"Inspect the diff.","model":null}`); id != "demo.reviewer" {
		t.Fatalf("canonical id = %q", id)
	}
	definitions := host.Subagents()
	if len(definitions) != 1 || definitions[0].ID != "demo.reviewer" || definitions[0].Model != "" || definitions[0].Owner != owner || definitions[0].SourcePath != filepath.Join(manifestRoot, "plugin.json") {
		t.Fatalf("definitions = %#v", definitions)
	}
	var duplicate *RPCError
	_, err := host.handleRequest(t.Context(), owner, "kit/subagents/register", json.RawMessage(`{"id":"reviewer","description":"Again","instructions":"Again"}`))
	if !errors.As(err, &duplicate) || duplicate.Code != -32003 {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := host.handleRequest(t.Context(), owner, "kit/subagents/unregister", json.RawMessage(`{"id":"reviewer"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := host.handleRequest(t.Context(), owner, "kit/subagents/unregister", json.RawMessage(`{"id":"reviewer"}`)); err != nil {
		t.Fatal(err)
	}
	if definitions := host.Subagents(); len(definitions) != 0 {
		t.Fatalf("definitions after unregister = %#v", definitions)
	}
}

func TestHostSubagentValidationBaseConflictsAndRevocation(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	first := t.TempDir()
	second := t.TempDir()
	hostInstallation(t, filepath.Join(first, ".kit", "plugins"), "demo", "demo", "normal")
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: first, Session: SessionContext{ID: "session-one"}, ReservedSubagents: []string{"demo.reserved"}})
	defer host.Close(context.Background())
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	owner := host.Instances()[0].ID
	for _, params := range []string{
		`{"id":"reserved","description":"Reserved","instructions":"No"}`,
		`{"id":"bad","description":"Bad","instructions":"   "}`,
		`{"id":"bad","description":"Bad","instructions":"ok","extra":true}`,
		`{"id":"` + strings.Repeat("x", 128) + `","description":"Bad","instructions":"ok"}`,
	} {
		if _, err := host.handleRequest(t.Context(), owner, "kit/subagents/register", json.RawMessage(params)); err == nil {
			t.Fatalf("accepted invalid registration: %s", params)
		}
	}
	registerSubagent(t, host, owner, `{"id":"temporary","description":"Temporary","instructions":"Work"}`)
	host.ChangeCWD(second)
	eventuallyHost(t, func() bool { return len(host.Subagents()) == 0 })
	if _, err := host.handleRequest(t.Context(), owner, "kit/subagents/unregister", json.RawMessage(`{"id":"temporary"}`)); err == nil {
		t.Fatal("revoked generation mutated subagent catalog")
	}
}

func TestHostAppliedBaseDefinitionRemovesPluginConflict(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	hostInstallation(t, home.Plugins, "demo", "demo", "normal")
	host := testHost(t, home, t.TempDir())
	host.Start()
	eventuallyHost(t, func() bool { return readyHost(host, 1) })
	owner := host.Instances()[0].ID
	registerSubagent(t, host, owner, `{"id":"reviewer","description":"Review","instructions":"Work"}`)
	host.SetSubagentBase([]string{"demo.reviewer"})
	if definitions := host.Subagents(); len(definitions) != 0 {
		t.Fatalf("conflicting plugin definition survived: %#v", definitions)
	}
	if warnings := host.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "conflicts with an applied filesystem definition") {
		t.Fatalf("warnings = %#v", warnings)
	}
	registerSubagent(t, host, owner, `{"id":"other","description":"Other","instructions":"Work"}`)
	base := make([]string, MaxSubagents)
	for index := range base {
		base[index] = fmt.Sprintf("base-%03d", index)
	}
	host.SetSubagentBase(base)
	if definitions := host.Subagents(); len(definitions) != 0 {
		t.Fatalf("plugin definition exceeded replacement base capacity: %#v", definitions)
	}
	if warnings := host.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "definition limit") {
		t.Fatalf("capacity warnings = %#v", warnings)
	}
}
