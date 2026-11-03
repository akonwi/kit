package droids

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// refreshedOpenAICatalog lists two OpenAI models: one the embedded catalog
// also lists and one it does not.
const refreshedOpenAICatalog = `{
	"openai":{"models":{
		"gpt-4o-mini":{
			"id":"gpt-4o-mini","name":"GPT-4o mini refreshed","family":"gpt-mini","tool_call":true,
			"reasoning":false,"modalities":{"input":["text","image"],"output":["text"]},
			"limit":{"context":256000,"output":32000},"cost":{"input":1,"output":2}
		},
		"gpt-new":{
			"id":"gpt-new","name":"GPT New","family":"gpt","tool_call":true,"reasoning":true,
			"modalities":{"input":["text"],"output":["text"]},
			"limit":{"context":400000,"input":272000,"output":128000},"cost":{"input":3,"output":12}
		}
	}},
	"anthropic":{"models":{}}
}`

func catalogServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func providerModelIDs(providers Providers, providerID string) []string {
	var ids []string
	for _, model := range providers.Models() {
		if model.Provider == providerID {
			ids = append(ids, model.ID)
		}
	}
	return ids
}

func TestRefreshModelsListsExactlyTheCatalogAndKeepsRetiredModelsResolvable(t *testing.T) {
	requests := make(chan string, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- string(body)
		http.Error(w, "test upstream", http.StatusTeapot)
	}))
	defer upstream.Close()
	catalog := catalogServer(t, refreshedOpenAICatalog)

	providers, err := NewProvidersWithOptions(context.Background(), RegistryOptions{ModelCatalogURL: catalog.URL},
		OpenAI{APIKey: "test", BaseURL: upstream.URL}, Anthropic{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	anthropicBefore := providerModelIDs(providers, "anthropic")
	retiredBefore, err := providers.Resolve("openai/gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if err := providers.RefreshModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got, want := providerModelIDs(providers, "openai"), []string{"gpt-4o-mini", "gpt-new"}; !slices.Equal(got, want) {
		t.Fatalf("listed OpenAI models = %v, want %v", got, want)
	}
	if got := providerModelIDs(providers, "anthropic"); !slices.Equal(got, anthropicBefore) {
		t.Fatalf("an empty provider catalog changed Anthropic's models: %v, want %v", got, anthropicBefore)
	}
	if _, ok := providers.Model("openai/gpt-4o"); ok {
		t.Fatal("Model lists a model the catalog retired")
	}

	// A runtime resolving the retired model, and one that resolved it before
	// the refresh, both still stream it.
	retired, err := providers.Resolve("openai/gpt-4o")
	if err != nil {
		t.Fatalf("resolve retired model: %v", err)
	}
	if retired.Name != retiredBefore.Name || retired.ContextWindow != retiredBefore.ContextWindow {
		t.Fatalf("retired model = %#v, want %#v", retired, retiredBefore)
	}
	for _, model := range []Model{retired, retiredBefore} {
		stream, err := model.boundProvider().Stream(context.Background(), model, Request{})
		if err != nil {
			t.Fatalf("bound provider rejected retired model: %v", err)
		}
		drainAssistantStream(stream)
		if body := <-requests; !strings.Contains(body, `"model":"gpt-4o"`) {
			t.Fatalf("upstream request = %s", body)
		}
	}
	drain(providers.Stream(context.Background(), retired, Request{}))
	if body := <-requests; !strings.Contains(body, `"model":"gpt-4o"`) {
		t.Fatalf("registry stream request = %s", body)
	}

	// A later catalog that lists the model again relists it.
	relisting := catalogServer(t, strings.Replace(refreshedOpenAICatalog, `"gpt-new":{
			"id":"gpt-new"`, `"gpt-4o":{
			"id":"gpt-4o"`, 1))
	if err := providers.(*registry).refreshModels(context.Background(), relisting.URL); err != nil {
		t.Fatal(err)
	}
	if got, want := providerModelIDs(providers, "openai"), []string{"gpt-4o", "gpt-4o-mini"}; !slices.Equal(got, want) {
		t.Fatalf("relisted OpenAI models = %v, want %v", got, want)
	}
	if _, err := providers.Resolve("openai/gpt-new"); err != nil {
		t.Fatalf("model retired by the second refresh no longer resolves: %v", err)
	}
}

func TestCachedModelCatalogListsOnlyCachedModels(t *testing.T) {
	cache := &memoryModelCatalogCache{data: []byte(cachedCatalogFixture)}
	providers, err := NewProvidersWithOptions(context.Background(), RegistryOptions{ModelCatalogCache: cache}, OpenAI{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := providerModelIDs(providers, "openai"), []string{"gpt-cache"}; !slices.Equal(got, want) {
		t.Fatalf("listed OpenAI models = %v, want %v", got, want)
	}
	if model, err := providers.Resolve("openai/gpt-4o"); err != nil || model.ID != "gpt-4o" {
		t.Fatalf("embedded model the cache omits = %#v, %v", model, err)
	}
}

func TestRefreshModelsRejectsInvalidPayloadAndKeepsCatalog(t *testing.T) {
	for name, body := range map[string]string{
		"not json":  `<html>maintenance</html>`,
		"truncated": `{"openai":{"models":{`,
	} {
		t.Run(name, func(t *testing.T) {
			catalog := catalogServer(t, body)
			cache := &memoryModelCatalogCache{data: []byte(cachedCatalogFixture)}
			providers, err := NewProvidersWithOptions(context.Background(), RegistryOptions{ModelCatalogCache: cache, ModelCatalogURL: catalog.URL}, OpenAI{})
			if err != nil {
				t.Fatal(err)
			}
			if err := providers.RefreshModels(context.Background()); err == nil {
				t.Fatal("invalid catalog was accepted")
			}
			if got, want := providerModelIDs(providers, "openai"), []string{"gpt-cache"}; !slices.Equal(got, want) {
				t.Fatalf("listed OpenAI models = %v, want %v", got, want)
			}
			if string(cache.data) != cachedCatalogFixture {
				t.Fatalf("invalid catalog replaced cache: %s", cache.data)
			}
		})
	}
}

func TestRefreshModelsStopsWhenItsContextEnds(t *testing.T) {
	release := make(chan struct{})
	catalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer catalog.Close()
	defer close(release)
	providers, err := NewProvidersWithOptions(context.Background(), RegistryOptions{ModelCatalogURL: catalog.URL}, OpenAI{})
	if err != nil {
		t.Fatal(err)
	}
	before := providerModelIDs(providers, "openai")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := providers.RefreshModels(ctx); err == nil {
		t.Fatal("refresh outlived its context")
	}
	if got := providerModelIDs(providers, "openai"); !slices.Equal(got, before) {
		t.Fatalf("listed OpenAI models = %v, want %v", got, before)
	}
}

func TestASlowRefreshDoesNotReplaceANewerCatalog(t *testing.T) {
	requested := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseSlow := func() { releaseOnce.Do(func() { close(release) }) }
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requested)
		<-release
		_, _ = io.WriteString(w, cachedCatalogFixture)
	}))
	defer slow.Close()
	defer releaseSlow()
	fast := catalogServer(t, refreshedOpenAICatalog)
	cache := &lockedModelCatalogCache{}
	providers, err := NewProvidersWithOptions(context.Background(), RegistryOptions{ModelCatalogCache: cache}, OpenAI{})
	if err != nil {
		t.Fatal(err)
	}
	registry := providers.(*registry)
	slowResult := make(chan error, 1)
	go func() { slowResult <- registry.refreshModels(context.Background(), slow.URL) }()
	<-requested
	if err := registry.refreshModels(context.Background(), fast.URL); err != nil {
		t.Fatal(err)
	}
	releaseSlow()
	if err := <-slowResult; err != nil {
		t.Fatal(err)
	}
	if got, want := providerModelIDs(providers, "openai"), []string{"gpt-4o-mini", "gpt-new"}; !slices.Equal(got, want) {
		t.Fatalf("listed OpenAI models = %v, want %v", got, want)
	}
	cached, err := parseModelCatalog(cache.data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cached["openai"].Models["gpt-new"]; !ok {
		t.Fatalf("the slow refresh replaced the cached catalog: %s", cache.data)
	}
}

// TestConcurrentRefreshesAndReads runs refreshes beside resolution, listing,
// and streaming, as a startup refresh and a client's manual refresh do while
// sessions configure; run it with -race.
func TestConcurrentRefreshesAndReads(t *testing.T) {
	catalog := catalogServer(t, refreshedOpenAICatalog)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "test upstream", http.StatusTeapot)
	}))
	defer upstream.Close()
	cache := &lockedModelCatalogCache{}
	providers, err := NewProvidersWithOptions(context.Background(), RegistryOptions{ModelCatalogCache: cache, ModelCatalogURL: catalog.URL}, OpenAI{APIKey: "test", BaseURL: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := providers.Resolve("openai/gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 4 {
		group.Add(2)
		go func() {
			defer group.Done()
			if err := providers.RefreshModels(context.Background()); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer group.Done()
			for range 20 {
				_ = providers.Models()
				_, _ = providers.Model("gpt-4o-mini")
				if _, err := providers.Resolve("openai/gpt-4o"); err != nil {
					t.Error(err)
				}
			}
			drain(providers.Stream(context.Background(), bound, Request{}))
		}()
	}
	group.Wait()
	if got, want := providerModelIDs(providers, "openai"), []string{"gpt-4o-mini", "gpt-new"}; !slices.Equal(got, want) {
		t.Fatalf("listed OpenAI models = %v, want %v", got, want)
	}
}

type lockedModelCatalogCache struct {
	mu   sync.Mutex
	data []byte
}

func (c *lockedModelCatalogCache) Load(context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.data...), nil
}

func (c *lockedModelCatalogCache) Store(_ context.Context, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = append(c.data[:0], data...)
	return nil
}

func drain(stream Stream) {
	for range stream.Events() {
	}
}

func drainAssistantStream(stream AssistantStream) {
	for range stream.Events() {
	}
	_ = stream.Close()
}
