package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
)

// refreshedCatalog lists two OpenAI models: one the embedded catalog lists
// and one it does not.
const refreshedCatalog = `{"openai":{"models":{
	"gpt-4o-mini":{"id":"gpt-4o-mini","name":"GPT-4o mini","family":"gpt-mini","tool_call":true,
		"modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":128000,"output":16384},"cost":{"input":1,"output":2}},
	"gpt-new":{"id":"gpt-new","name":"GPT New","family":"gpt","tool_call":true,
		"modalities":{"input":["text"],"output":["text"]},"limit":{"context":400000,"output":128000},"cost":{"input":3,"output":12}}
}}}`

// logBuffer collects log output written from several goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type catalogDaemon struct {
	paths  apphome.Paths
	client *clienttransport.Client
	logs   *logBuffer
	cancel context.CancelFunc
	result chan error
}

// startCatalogDaemon runs a daemon whose OpenAI catalog refreshes from catalog.
func startCatalogDaemon(t *testing.T, catalog http.Handler) *catalogDaemon {
	t.Helper()
	server := httptest.NewServer(catalog)
	t.Cleanup(server.Close)
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	providers, err := droids.NewProvidersWithOptions(context.Background(), droids.RegistryOptions{
		ModelCatalogCache: droids.FileModelCatalogCache{Path: paths.ModelCatalogCache},
		ModelCatalogURL:   server.URL,
	}, droids.OpenAI{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	daemon := &catalogDaemon{paths: paths, client: localTransport(paths), logs: logs, cancel: cancel, result: make(chan error, 1)}
	go func() {
		daemon.result <- Run(ctx, RunOptions{
			Paths: paths, Providers: providers, RefreshModelCatalog: true,
			Logger: slog.New(slog.NewTextHandler(logs, nil)),
		})
	}()
	t.Cleanup(func() {
		daemon.client.CloseIdleConnections()
		daemon.stop(t)
	})
	return daemon
}

// stop cancels the daemon and returns how long Run took to return.
func (d *catalogDaemon) stop(t *testing.T) time.Duration {
	t.Helper()
	started := time.Now()
	d.cancel()
	select {
	case err, ok := <-d.result:
		if !ok {
			return 0
		}
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
		close(d.result)
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop")
	}
	return time.Since(started)
}

func (d *catalogDaemon) waitReady(t *testing.T) {
	t.Helper()
	eventually(t, func() bool { _, _, err := d.client.Probe(t.Context(), true); return err == nil })
}

func (d *catalogDaemon) openAIModels(t *testing.T) []string {
	t.Helper()
	catalog, err := d.client.ListModels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, model := range catalog.Models {
		if model.Provider == "openai" {
			ids = append(ids, model.ID)
		}
	}
	return ids
}

func embeddedOpenAIModels(t *testing.T) []string {
	t.Helper()
	providers, err := droids.NewProviders(droids.OpenAI{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, model := range providers.Models() {
		ids = append(ids, model.Provider+"/"+model.ID)
	}
	return ids
}

func TestRunRefreshesModelCatalogInTheBackground(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	requested := make(chan struct{}, 2)
	daemon := startCatalogDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, refreshedCatalog)
	}))

	// The daemon serves while the startup refresh is still waiting on the
	// catalog, with the embedded catalog.
	<-requested
	daemon.waitReady(t)
	if got, want := daemon.openAIModels(t), embeddedOpenAIModels(t); !slices.Equal(got, want) {
		t.Fatalf("models before refresh = %v, want %v", got, want)
	}

	// A client's manual refresh runs alongside the startup refresh.
	manual := make(chan error, 1)
	go func() {
		_, err := daemon.client.RefreshModels(context.Background())
		manual <- err
	}()
	<-requested
	close(release)
	if err := <-manual; err != nil {
		t.Fatalf("manual refresh: %v", err)
	}
	want := []string{"openai/gpt-4o-mini", "openai/gpt-new"}
	eventually(t, func() bool { return strings.Contains(daemon.logs.String(), "msg=\"model catalog refreshed\" models=2") })
	if got := daemon.openAIModels(t); !slices.Equal(got, want) {
		t.Fatalf("models after refresh = %v, want %v", got, want)
	}
	cached, err := os.ReadFile(daemon.paths.ModelCatalogCache)
	if err != nil || !strings.Contains(string(cached), `"gpt-new"`) {
		t.Fatalf("cached catalog = %s, %v", cached, err)
	}
}

func TestRunKeepsCatalogWhenStartupRefreshFails(t *testing.T) {
	t.Parallel()
	for name, handler := range map[string]http.HandlerFunc{
		"http error": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		},
		"invalid payload": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `<html>maintenance</html>`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			daemon := startCatalogDaemon(t, handler)
			daemon.waitReady(t)
			eventually(t, func() bool {
				return strings.Contains(daemon.logs.String(), `level=WARN msg="model catalog refresh failed; keeping the current catalog"`)
			})
			if got, want := daemon.openAIModels(t), embeddedOpenAIModels(t); !slices.Equal(got, want) {
				t.Fatalf("models after failed refresh = %v, want %v", got, want)
			}
			if _, err := os.Stat(daemon.paths.ModelCatalogCache); !os.IsNotExist(err) {
				t.Fatalf("failed refresh wrote the catalog cache: %v", err)
			}
		})
	}
}

func TestRunCancelsModelCatalogRefreshOnShutdown(t *testing.T) {
	t.Parallel()
	requested := make(chan struct{})
	canceled := make(chan struct{})
	daemon := startCatalogDaemon(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(requested)
		<-r.Context().Done()
		close(canceled)
	}))
	<-requested
	daemon.waitReady(t)
	if elapsed := daemon.stop(t); elapsed >= defaultModelCatalogRefresh.Timeout {
		t.Fatalf("shutdown waited %v for the catalog refresh", elapsed)
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel the catalog request")
	}
}

func TestModelCatalogRefreshTimesOut(t *testing.T) {
	t.Parallel()
	catalog := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer catalog.Close()
	providers, err := droids.NewProvidersWithOptions(context.Background(), droids.RegistryOptions{ModelCatalogURL: catalog.URL}, droids.OpenAI{})
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuffer{}
	stop := startModelCatalogRefresh(context.Background(), providers,
		modelCatalogRefreshPolicy{Timeout: 50 * time.Millisecond, Interval: time.Hour, Check: time.Hour},
		slog.New(slog.NewTextHandler(logs, nil)))
	defer stop()
	eventually(t, func() bool {
		return strings.Contains(logs.String(), `level=WARN msg="model catalog refresh timed out; keeping the current catalog" timeout=50ms`)
	})
	var ids []string
	for _, model := range providers.Models() {
		ids = append(ids, model.Provider+"/"+model.ID)
	}
	if want := embeddedOpenAIModels(t); !slices.Equal(ids, want) {
		t.Fatalf("models after timeout = %v, want %v", ids, want)
	}
}

func TestModelCatalogRefreshRetriesFailuresAndRepeatsAfterInterval(t *testing.T) {
	t.Parallel()
	const interval = time.Second
	var (
		mu    sync.Mutex
		times []time.Time
	)
	third := make(chan struct{})
	catalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		count := len(times)
		mu.Unlock()
		switch count {
		case 1:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		case 3:
			close(third)
		}
		_, _ = io.WriteString(w, refreshedCatalog)
	}))
	defer catalog.Close()
	providers, err := droids.NewProvidersWithOptions(context.Background(), droids.RegistryOptions{ModelCatalogURL: catalog.URL}, droids.OpenAI{})
	if err != nil {
		t.Fatal(err)
	}
	stop := startModelCatalogRefresh(context.Background(), providers,
		modelCatalogRefreshPolicy{Timeout: time.Second, Interval: interval, Check: 10 * time.Millisecond},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	select {
	case <-third:
	case <-time.After(10 * time.Second):
		t.Fatal("the catalog was not refreshed again after its interval")
	}
	stop()
	mu.Lock()
	defer mu.Unlock()
	if retry := times[1].Sub(times[0]); retry >= interval {
		t.Fatalf("a failed refresh was retried after %v, want the next check", retry)
	}
	if repeat := times[2].Sub(times[1]); repeat < interval {
		t.Fatalf("a successful refresh repeated after %v, want at least %v", repeat, interval)
	}
}

func TestASessionKeepsAModelTheCatalogRetires(t *testing.T) {
	t.Parallel()
	var (
		mu       sync.Mutex
		requests int
	)
	daemon := startCatalogDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requests++
		first := requests == 1
		mu.Unlock()
		if first {
			// The startup refresh fails, so the session starts on the
			// embedded catalog.
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, refreshedCatalog)
	}))
	daemon.waitReady(t)
	eventually(t, func() bool { return strings.Contains(daemon.logs.String(), "model catalog refresh failed") })
	created, err := daemon.client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "openai/gpt-4o"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.client.RefreshModels(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, want := daemon.openAIModels(t), []string{"openai/gpt-4o-mini", "openai/gpt-new"}; !slices.Equal(got, want) {
		t.Fatalf("listed models = %v, want %v", got, want)
	}

	textAndImage := []protocol.ModelInputKind{protocol.ModelInputText, protocol.ModelInputImage}
	snapshot, err := daemon.client.GetSessionSnapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("snapshot a session on a retired model: %v", err)
	}
	if snapshot.Session.Model != "openai/gpt-4o" || !slices.Equal(snapshot.Session.Inputs, textAndImage) {
		t.Fatalf("session on a retired model = %+v", snapshot.Session)
	}
	if _, err := daemon.client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "openai/gpt-4o"}); err == nil {
		t.Fatal("created a session on a retired model")
	}
	off := protocol.ThinkingLevel("off")
	kept, err := daemon.client.ConfigureSession(t.Context(), created.ID, protocol.ConfigureSessionInput{
		ExpectedRevision: snapshot.Session.ConfigurationRevision, Model: "openai/gpt-4o", ThinkingLevel: &off,
	})
	if err != nil {
		t.Fatalf("set the thinking level of a session on a retired model: %v", err)
	}
	if kept.Session.Model != "openai/gpt-4o" || kept.Session.ThinkingLevel != "off" {
		t.Fatalf("session after a thinking change = %+v", kept.Session)
	}
	configured, err := daemon.client.ConfigureSession(t.Context(), created.ID, protocol.ConfigureSessionInput{
		ExpectedRevision: kept.Session.ConfigurationRevision, Model: "openai/gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("switch a session off a retired model: %v", err)
	}
	if configured.Session.Model != "openai/gpt-4o-mini" {
		t.Fatalf("configured session = %+v", configured.Session)
	}
	_, err = daemon.client.ConfigureSession(t.Context(), created.ID, protocol.ConfigureSessionInput{
		ExpectedRevision: configured.Session.ConfigurationRevision, Model: "openai/gpt-4o",
	})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("switching back to a retired model = %v, want an invalid request", err)
	}
}
