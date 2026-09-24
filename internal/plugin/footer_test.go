package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

func footerCall(t *testing.T, h *Host, owner InstanceID, method, params string) string {
	t.Helper()
	result, err := h.handleRequest(t.Context(), owner, "kit/footer/"+method, json.RawMessage(params))
	if err != nil {
		t.Fatal(err)
	}
	return string(result)
}

func TestFooterRegistrationOrderUpdatesAndSnapshotIsolation(t *testing.T) {
	h, _ := commandHost(t)
	owner := findCommand(t, h, "demo.ok").Owner
	if got := footerCall(t, h, owner, "set", `{"id":"first","content":[{"text":"Ready ","style":{"fg":"toolText","bold":true}},{"text":"now"}]}`); got != `{"id":"demo.first"}` {
		t.Fatalf("set result = %s", got)
	}
	footerCall(t, h, owner, "set", `{"id":"second","side":"right","clickable":false,"content":[{"text":"Next"}]}`)
	footerCall(t, h, owner, "set", `{"id":"first","content":[{"text":"Updated","style":{"bg":"bgMuted","dim":true,"italic":true,"underline":true,"strikethrough":true}}]}`)
	view := h.Footer()
	if len(view.Items) != 2 || view.Items[0].ID != "demo.first" || view.Items[1].ID != "demo.second" || view.Items[0].Content[0].Text != "Updated" {
		t.Fatalf("footer order = %#v", view)
	}
	if view.Items[0].Owner != owner || view.Items[0].Content[0].Style.BG != "bgMuted" || !view.Items[0].Content[0].Style.Strikethrough {
		t.Fatalf("owned styling = %#v", view.Items[0])
	}
	view.Items[0].Content[0].Text = "mutated client copy"
	if h.Footer().Items[0].Content[0].Text != "Updated" {
		t.Fatal("snapshot aliases host contribution")
	}
	for range 2 {
		footerCall(t, h, owner, "clear", `{"id":"first"}`)
	}
	footerCall(t, h, owner, "set", `{"id":"first","content":[{"text":"Recreated"}]}`)
	if h.Footer().Items[1].ID != "demo.first" {
		t.Fatal("recreated item did not append")
	}
}

func TestFooterClaimsAreGenerationOwnedAndRestoreLocationOnRevocation(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd := t.TempDir()
	hostInstallation(t, home.Plugins, "user", "user-plugin", "commands")
	hostInstallation(t, filepath.Join(cwd, ".kit", "plugins"), "project", "project-plugin", "commands")
	h := testHost(t, home, cwd)
	h.Start()
	eventuallyHost(t, func() bool { return len(h.Commands()) == 8 })
	user := findCommand(t, h, "user-plugin.ok").Owner
	project := findCommand(t, h, "project-plugin.ok").Owner
	footerCall(t, h, user, "set", `{"id":"status","content":[{"text":"User"}]}`)
	footerCall(t, h, project, "set", `{"id":"status","content":[{"text":"Project"}]}`)
	for _, owner := range []InstanceID{user, project} {
		footerCall(t, h, owner, "hide", `{"id":"kit.footer.location"}`)
	}
	footerCall(t, h, user, "hide", `{"id":"project-plugin.status"}`)
	if view := h.Footer(); !view.LocationHidden || len(view.Items) != 1 || view.Items[0].ID != "user-plugin.status" {
		t.Fatalf("hidden projection = %#v", view)
	}
	footerCall(t, h, user, "show", `{"id":"kit.footer.location"}`)
	if !h.Footer().LocationHidden {
		t.Fatal("show removed another owner's claim")
	}
	h.ChangeCWD(t.TempDir())
	if view := h.Footer(); view.LocationHidden || len(view.Items) != 1 || view.Items[0].Owner != user {
		t.Fatalf("cwd revocation = %#v", view)
	}
	footerCall(t, h, user, "hide", `{"id":"kit.footer.location"}`)
	h.Reload()
	if view := h.Footer(); view.LocationHidden || len(view.Items) != 0 {
		t.Fatalf("reload projection = %#v", view)
	}
	if _, err := h.handleRequest(t.Context(), project, "kit/footer/set", json.RawMessage(`{"id":"status","content":[{"text":"Stale"}]}`)); err == nil {
		t.Fatal("revoked project owner mutated footer")
	}
}

func TestFooterRejectsUnsafeAndUnsupportedParameters(t *testing.T) {
	h, _ := commandHost(t)
	owner := findCommand(t, h, "demo.ok").Owner
	for _, params := range []string{
		`{}`, `{"id":"x","content":[]}`, `{"id":"x","content":null}`, `{"id":"x","content":[{"text":null}]}`,
		`{"id":"x","content":[{"text":"ok","text":"duplicate"}]}`,
		`{"id":"x","content":[{"text":"\u001b[2J"}]}`,
		`{"id":"x","content":[{"text":"new\nline"}]}`,
		`{"id":"x","content":[{"text":"ok","style":{"fg":"#ff0000"}}]}`,
		`{"id":"x","content":[{"text":"ok","style":{"bold":null}}]}`,
		`{"id":"x","content":[{"text":"ok","style":{"bold":true,"bold":false}}]}`,
		`{"id":"x","content":[{"text":"ok"}],"unknown":true}`,
		`{"id":"x","content":[{"text":"ok"}],"side":"left"}`,
		`{"id":"x","content":[{"text":"ok"}],"clickable":true}`,
		`{"id":"x","content":[{"text":"ok"}],"action":{"type":"open-url","url":"https://example.com"}}`,
	} {
		if _, err := h.handleRequest(t.Context(), owner, "kit/footer/set", json.RawMessage(params)); err == nil {
			t.Fatalf("accepted %s", params)
		}
	}
	for _, target := range []string{"kit.header.title", "kit.footer.status", "kit-evil.item", "bad", "demo..status"} {
		if _, err := h.handleRequest(t.Context(), owner, "kit/footer/hide", json.RawMessage(fmt.Sprintf(`{"id":%q}`, target))); err == nil {
			t.Fatalf("accepted protected/invalid target %q", target)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := h.handleRequest(ctx, owner, "kit/footer/hide", json.RawMessage(`{"id":"kit.footer.location"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mutation = %v", err)
	}
	if view := h.Footer(); view.LocationHidden || len(view.Items) != 0 {
		t.Fatalf("invalid requests mutated footer = %#v", view)
	}
}

func TestFooterResourceBoundsAndIdempotentClaims(t *testing.T) {
	h, _ := commandHost(t)
	owner := findCommand(t, h, "demo.ok").Owner
	for i := range MaxFooterItems {
		footerCall(t, h, owner, "set", fmt.Sprintf(`{"id":"item%d","content":[{"text":"ok"}]}`, i))
	}
	_, err := h.handleRequest(t.Context(), owner, "kit/footer/set", json.RawMessage(`{"id":"overflow","content":[{"text":"ok"}]}`))
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32005 {
		t.Fatalf("item limit = %v", err)
	}
	footerCall(t, h, owner, "set", `{"id":"item0","content":[{"text":"Updating a full catalog is allowed"}]}`)
	for i := range MaxFooterClaims {
		footerCall(t, h, owner, "hide", fmt.Sprintf(`{"id":"demo.future%d"}`, i))
	}
	footerCall(t, h, owner, "hide", `{"id":"demo.future0"}`)
	_, err = h.handleRequest(t.Context(), owner, "kit/footer/hide", json.RawMessage(`{"id":"demo.extra"}`))
	if !errors.As(err, &failure) || failure.Code != -32005 {
		t.Fatalf("claim limit = %v", err)
	}
	footerCall(t, h, owner, "show", `{"id":"demo.future0"}`)
	footerCall(t, h, owner, "hide", `{"id":"demo.extra"}`)
	_, err = parseFooterItem(json.RawMessage(`{"id":"x","content":[` + strings.Repeat(`{"text":""},`, MaxFooterSegments) + `{"text":""}]}`))
	if !errors.As(err, &failure) || failure.Code != -32005 {
		t.Fatalf("segment limit = %v", err)
	}
	_, err = parseFooterItem(json.RawMessage(fmt.Sprintf(`{"id":"x","content":[{"text":%q},{"text":"x"}]}`, strings.Repeat("a", MaxFooterTextBytes))))
	if !errors.As(err, &failure) || failure.Code != -32005 {
		t.Fatalf("text limit = %v", err)
	}
}

func TestFooterThemeTokensMatchPublicSchema(t *testing.T) {
	data, err := os.ReadFile("../../app/docs/plugin-protocol/protocol.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	expected := make(map[string]bool)
	for _, token := range schema.Defs["ThemeToken"].Enum {
		expected[token] = true
	}
	if !reflect.DeepEqual(footerThemeTokens, expected) {
		t.Fatalf("footer tokens differ from public schema")
	}
}

func TestFooterSubprocessPublishesChangesAndCrashRevokesAllChrome(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	root := hostInstallation(t, home.Plugins, "footer", "demo", "footer")
	var changes atomic.Int64
	h := NewHost(t.Context(), HostConfig{Home: home, CWD: t.TempDir(), Session: SessionContext{ID: "session-one"}, Changed: func() { changes.Add(1) }})
	t.Cleanup(func() {
		if err := h.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	h.Start()
	eventuallyHost(t, func() bool {
		view := h.Footer()
		return view.LocationHidden && len(view.Items) == 1 && changes.Load() > 0
	})
	eventuallyHost(t, func() bool {
		result, err := os.ReadFile(filepath.Join(root, "footer-result.json"))
		return err == nil && string(result) == `{"id":"demo.status"}`
	})
	view := h.Footer()
	if view.Items[0].Content[0].Text != "Plugin ready" || view.Items[0].Content[0].Style.FG != "toolText" || !view.Items[0].Content[0].Style.Bold {
		t.Fatalf("subprocess footer = %#v", view)
	}
	before := changes.Load()
	h.mu.Lock()
	instance := h.activeEntryLocked(view.Items[0].Owner).instance
	h.mu.Unlock()
	_, _ = instance.Call(t.Context(), "exit", nil)
	eventuallyHost(t, func() bool {
		view := h.Footer()
		return !view.LocationHidden && len(view.Items) == 0 && changes.Load() > before
	})
}
