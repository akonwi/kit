package savedservers

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "home", "servers.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store, path
}

func TestAddListFindRemove(t *testing.T) {
	t.Parallel()
	store, path := newTestStore(t)

	servers, err := store.List()
	if err != nil || len(servers) != 0 {
		t.Fatalf("List() on missing file = %v, %v; want none", servers, err)
	}
	for _, server := range []Server{{Name: "vps", Home: "/srv/kit"}, {Name: "dev", Home: "/tmp/kit-dev"}} {
		if err := store.Add(server); err != nil {
			t.Fatalf("Add(%q) error = %v", server.Name, err)
		}
	}
	servers, err = store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []Server{{Name: "dev", Home: "/tmp/kit-dev"}, {Name: "vps", Home: "/srv/kit"}}
	if !reflect.DeepEqual(servers, want) {
		t.Fatalf("List() = %v, want %v", servers, want)
	}
	found, err := store.Find("vps")
	if err != nil || found != (Server{Name: "vps", Home: "/srv/kit"}) {
		t.Fatalf("Find(vps) = %v, %v", found, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	wantFile := `{
  "servers": [
    {
      "name": "dev",
      "home": "/tmp/kit-dev"
    },
    {
      "name": "vps",
      "home": "/srv/kit"
    }
  ]
}
`
	if string(data) != wantFile {
		t.Fatalf("file =\n%s\nwant\n%s", data, wantFile)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}

	if err := store.Remove("dev"); err != nil {
		t.Fatalf("Remove(dev) error = %v", err)
	}
	servers, _ = store.List()
	if !reflect.DeepEqual(servers, []Server{{Name: "vps", Home: "/srv/kit"}}) {
		t.Fatalf("List() after remove = %v", servers)
	}
}

func TestAddRejectsDuplicatesAndInvalidServers(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if err := store.Add(Server{Name: "dev", Home: "/tmp/a"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Add(Server{Name: "dev", Home: "/tmp/b"}); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate Add() error = %v, want ErrExists", err)
	}
	for _, server := range []Server{
		{Name: "local", Home: "/tmp/a"},
		{Name: "", Home: "/tmp/a"},
		{Name: "Dev", Home: "/tmp/a"},
		{Name: "-dev", Home: "/tmp/a"},
		{Name: "my server", Home: "/tmp/a"},
		{Name: strings.Repeat("a", 33), Home: "/tmp/a"},
		{Name: "rel", Home: "relative/home"},
		{Name: "unclean", Home: "/tmp/../tmp/a"},
	} {
		if err := store.Add(server); err == nil {
			t.Errorf("Add(%+v) succeeded, want an error", server)
		}
	}
	if err := store.Add(Server{Name: "a.b-c_9", Home: "/tmp/c"}); err != nil {
		t.Fatalf("Add(a.b-c_9) error = %v", err)
	}
}

func TestFindAndRemoveReportMissingNames(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if _, err := store.Find("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find() error = %v, want ErrNotFound", err)
	}
	if err := store.Remove("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Remove() error = %v, want ErrNotFound", err)
	}
}

func TestListRejectsInvalidFiles(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"malformed": `{"servers": [`,
		"duplicate": `{"servers": [{"name": "a", "home": "/a"}, {"name": "a", "home": "/b"}]}`,
		"invalid":   `{"servers": [{"name": "local", "home": "/a"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store, path := newTestStore(t)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.List(); err == nil {
				t.Fatal("List() succeeded, want an error")
			}
		})
	}
}
