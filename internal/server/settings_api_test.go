package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
)

// reasoningEchoProviders serve the echo models, with test/echo reasoning at
// low, medium, and high; test/echo-alt has no reasoning.
type reasoningEchoProviders struct{ *daemonEchoProviders }

func (p reasoningEchoProviders) Models() []droids.Model {
	models := p.daemonEchoProviders.Models()
	models[0].Reasoning, models[0].ReasoningLevels = true, []string{"low", "medium", "high"}
	return models
}

func (p reasoningEchoProviders) Model(id string) (droids.Model, bool) {
	for _, model := range p.Models() {
		if id == model.ID || id == model.Provider+"/"+model.ID {
			return model, true
		}
	}
	return droids.Model{}, false
}

func (p reasoningEchoProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProviderWithImagePolicy("test", p.Models(), p.Stream, daemonEchoImagePolicy), model)
}

// startSettingsServer runs a server whose settings file starts as document,
// when it is not empty.
func startSettingsServer(t *testing.T, document string) (*clienttransport.Client, apphome.Paths) {
	t.Helper()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if document != "" {
		if err := os.WriteFile(paths.Settings, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, RunOptions{
			Paths: paths, Providers: reasoningEchoProviders{&daemonEchoProviders{}},
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-result:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	client := localTransport(paths)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, err := client.Probe(t.Context(), true); err == nil {
			return client, paths
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServerSettingsAreReadAndChangedThroughTheAPI(t *testing.T) {
	client, paths := startSettingsServer(t, `{"theme":"cozy","diffWrapLines":true}`)
	current, err := client.ServerSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := protocol.ServerSettings{
		ModelOverrides: []protocol.ModelOverride{}, PromptCacheRetention: protocol.PromptCacheShort, ReadClaudeConfigs: true,
		NewSessionModel: "test/echo", NewSessionThinkingLevel: protocol.ThinkingMedium,
	}
	if !reflect.DeepEqual(current, want) {
		t.Fatalf("initial settings = %+v\nwant %+v", current, want)
	}

	alternate, long, off := "test/echo-alt", protocol.PromptCacheLong, false
	compaction := "test/echo"
	updated, err := client.UpdateServerSettings(t.Context(), protocol.UpdateServerSettingsInput{
		DefaultModel: &alternate, CompactionModel: &compaction,
		ModelOverrides:       []protocol.ModelOverride{{Model: "test/echo", ContextWindow: 64_000}},
		PromptCacheRetention: &long, ReadClaudeConfigs: &off,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A new session would start on the default model, which has no
	// reasoning.
	want = protocol.ServerSettings{
		DefaultModel: "test/echo-alt", CompactionModel: "test/echo",
		ModelOverrides:       []protocol.ModelOverride{{Model: "test/echo", ContextWindow: 64_000}},
		PromptCacheRetention: protocol.PromptCacheLong, ReadClaudeConfigs: false,
		NewSessionModel: "test/echo-alt", NewSessionThinkingLevel: protocol.ThinkingOff,
	}
	if !reflect.DeepEqual(updated, want) {
		t.Fatalf("updated settings = %+v\nwant %+v", updated, want)
	}
	if reread, err := client.ServerSettings(t.Context()); err != nil || !reflect.DeepEqual(reread, want) {
		t.Fatalf("reread settings = %+v, %v", reread, err)
	}
	// The override applies at once, and the client's preferences in the same
	// file are kept.
	catalog, err := client.ListModels(t.Context())
	if err != nil || len(catalog.Models) == 0 || catalog.Models[0].ID != "test/echo" || catalog.Models[0].ContextWindow != 64_000 {
		t.Fatalf("catalog = %+v, %v", catalog, err)
	}
	document, err := os.ReadFile(paths.Settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{`"theme": "cozy"`, `"diffWrapLines": true`} {
		if !strings.Contains(string(document), kept) {
			t.Fatalf("settings file lost %s:\n%s", kept, document)
		}
	}

	// Clearing the default model falls back to the server's choice; removing
	// an override does not need its model to exist.
	cleared := ""
	updated, err = client.UpdateServerSettings(t.Context(), protocol.UpdateServerSettingsInput{
		DefaultModel:   &cleared,
		ModelOverrides: []protocol.ModelOverride{{Model: "test/echo", ContextWindow: 0}, {Model: "gone/model", ContextWindow: 0}},
	})
	if err != nil || updated.DefaultModel != "" || len(updated.ModelOverrides) != 0 ||
		updated.NewSessionModel != "test/echo" || updated.NewSessionThinkingLevel != protocol.ThinkingMedium {
		t.Fatalf("cleared settings = %+v, %v", updated, err)
	}

	unknown := "test/missing"
	for name, input := range map[string]protocol.UpdateServerSettingsInput{
		"unknown default model": {DefaultModel: &unknown},
		"unknown override":      {ModelOverrides: []protocol.ModelOverride{{Model: "test/missing", ContextWindow: 1}}},
	} {
		var failure *httpapi.APIError
		if _, err := client.UpdateServerSettings(t.Context(), input); !errors.As(err, &failure) || failure.Code != "invalid_request" {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

func TestNewSessionsStartWithTheServersDefaultModel(t *testing.T) {
	client, _ := startSettingsServer(t, `{"defaultModel":"test/echo-alt"}`)
	cwd := t.TempDir()
	for _, test := range []struct {
		name            string
		input           protocol.CreateSessionInput
		model, thinking string
	}{
		{name: "no model named", input: protocol.CreateSessionInput{CWD: cwd}, model: "test/echo-alt", thinking: "off"},
		{name: "a model named", input: protocol.CreateSessionInput{CWD: cwd, Model: "test/echo"}, model: "test/echo", thinking: "medium"},
		{name: "a model and thinking named", input: protocol.CreateSessionInput{CWD: cwd, Model: "test/echo", ThinkingLevel: "high"}, model: "test/echo", thinking: "high"},
	} {
		created, err := client.CreateSession(t.Context(), test.input)
		if err != nil || created.Model != test.model || created.ThinkingLevel != test.thinking {
			t.Errorf("%s: created %s at %q, %v; want %s at %q", test.name, created.Model, created.ThinkingLevel, err, test.model, test.thinking)
		}
	}
}

// A settings file the server cannot parse is a conflict for the settings
// API, but new sessions still start, with the built-in defaults.
func TestAnUnreadableSettingsFileIsAConflictButSessionsStillStart(t *testing.T) {
	client, _ := startSettingsServer(t, `{"theme":`)
	var failure *httpapi.APIError
	if _, err := client.ServerSettings(t.Context()); !errors.As(err, &failure) || failure.Code != "conflict" ||
		failure.Message != "the server's settings.json needs fixing; its log says why" {
		t.Fatalf("error = %v", err)
	}
	cleared := ""
	if _, err := client.UpdateServerSettings(t.Context(), protocol.UpdateServerSettingsInput{DefaultModel: &cleared}); !errors.As(err, &failure) || failure.Code != "conflict" {
		t.Fatalf("update error = %v", err)
	}
	for _, input := range []protocol.CreateSessionInput{{CWD: t.TempDir()}, {CWD: t.TempDir(), Model: "test/echo-alt"}} {
		if created, err := client.CreateSession(t.Context(), input); err != nil || created.Model == "" {
			t.Errorf("CreateSession(%+v) = %+v, %v", input, created, err)
		}
	}
}
