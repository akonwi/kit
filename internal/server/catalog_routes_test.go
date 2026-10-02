package server

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Every public route must be registered through internal/httpapi so the
// executable server and published contract cannot silently diverge.
func TestPublicRoutesUseCatalogRegistration(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	direct := regexp.MustCompile(`\.HandleFunc\("(?:GET|POST|PUT|PATCH|DELETE) `)
	for _, path := range matches {
		if filepath.Ext(path) != ".go" || path == "catalog_routes_test.go" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if direct.Match(data) {
			t.Errorf("%s registers a public route outside the HTTP catalog", path)
		}
	}
}
