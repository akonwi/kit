package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
)

func TestPromptCacheRetentionFollowsSettings(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	retention := promptCacheRetention(paths)
	if got := retention(); got != droids.PromptCacheShort {
		t.Fatalf("retention without settings = %q, want %q", got, droids.PromptCacheShort)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Settings), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		document string
		want     droids.PromptCacheRetention
	}{
		{`{"promptCacheRetention":"long"}`, droids.PromptCacheLong},
		{`{"promptCacheRetention":"short"}`, droids.PromptCacheShort},
		{`{"promptCacheRetention":"forever"}`, droids.PromptCacheShort},
		{`not json`, droids.PromptCacheShort},
	} {
		if err := os.WriteFile(paths.Settings, []byte(test.document), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := retention(); got != test.want {
			t.Fatalf("retention for %s = %q, want %q", test.document, got, test.want)
		}
	}
}
