package droids

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const cachedCatalogFixture = `{"openai":{"models":{"gpt-cache":{"id":"gpt-cache","name":"GPT Cache","family":"gpt","tool_call":true,"modalities":{"input":["text"],"output":["text"]},"limit":{"context":100000,"output":10000},"cost":{"input":1,"output":2}}}}}`

type memoryModelCatalogCache struct {
	data     []byte
	loadErr  error
	storeErr error
}

func (c *memoryModelCatalogCache) Load(context.Context) ([]byte, error) {
	return append([]byte(nil), c.data...), c.loadErr
}

func (c *memoryModelCatalogCache) Store(_ context.Context, data []byte) error {
	if c.storeErr != nil {
		return c.storeErr
	}
	c.data = append(c.data[:0], data...)
	return nil
}

func TestProvidersLoadCachedModelCatalog(t *testing.T) {
	cache := &memoryModelCatalogCache{data: []byte(cachedCatalogFixture)}
	providers, err := NewProvidersWithOptions(context.Background(), RegistryOptions{ModelCatalogCache: cache}, OpenAI{})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("openai/gpt-cache")
	if !ok || model.Name != "GPT Cache" || model.ContextWindow != 100_000 {
		t.Fatalf("cached model = %#v, %v", model, ok)
	}
}

func TestProvidersIgnoreUnavailableOrInvalidModelCatalogCache(t *testing.T) {
	for _, cache := range []*memoryModelCatalogCache{
		{loadErr: errors.New("unavailable")},
		{data: []byte("not json")},
	} {
		providers, err := NewProvidersWithOptions(context.Background(), RegistryOptions{ModelCatalogCache: cache}, OpenAI{})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := providers.Model("openai/gpt-4o-mini"); !ok {
			t.Fatal("embedded catalog was not retained")
		}
	}
}

func TestFileModelCatalogCacheRejectsNonRegularFile(t *testing.T) {
	cache := FileModelCatalogCache{Path: t.TempDir()}
	if _, err := cache.Load(context.Background()); err == nil {
		t.Fatal("directory cache was accepted")
	}
}

func TestFileModelCatalogCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cache", "models.dev.json")
	cache := FileModelCatalogCache{Path: path}
	if err := cache.Store(context.Background(), []byte(cachedCatalogFixture)); err != nil {
		t.Fatal(err)
	}
	data, err := cache.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != cachedCatalogFixture {
		t.Fatalf("cached data = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode = %o, want 600", info.Mode().Perm())
	}
}
