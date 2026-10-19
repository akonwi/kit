package savedservers

import (
	"encoding/json"
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
	for _, server := range []Server{{Name: "vps", Kind: KindURL, URL: "https://kit.example.ts.net"}, {Name: "dev", Kind: KindHome, Home: "/tmp/kit-dev"}} {
		if err := store.Add(server); err != nil {
			t.Fatalf("Add(%q) error = %v", server.Name, err)
		}
	}
	servers, err = store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []Server{{Name: "dev", Kind: KindHome, Home: "/tmp/kit-dev"}, {Name: "vps", Kind: KindURL, URL: "https://kit.example.ts.net"}}
	if !reflect.DeepEqual(servers, want) {
		t.Fatalf("List() = %v, want %v", servers, want)
	}
	found, err := store.Find("vps")
	if err != nil || found != (Server{Name: "vps", Kind: KindURL, URL: "https://kit.example.ts.net"}) {
		t.Fatalf("Find(vps) = %v, %v", found, err)
	}
	if found, err := store.FindURL("https://kit.example.ts.net"); err != nil || found.Name != "vps" {
		t.Fatalf("FindURL() = %v, %v", found, err)
	}
	if _, err := store.FindURL("https://other.example"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindURL(other) error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	wantFile := `{
  "servers": [
    {
      "name": "dev",
      "kind": "home",
      "home": "/tmp/kit-dev"
    },
    {
      "name": "vps",
      "kind": "url",
      "url": "https://kit.example.ts.net"
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
	if !reflect.DeepEqual(servers, []Server{{Name: "vps", Kind: KindURL, URL: "https://kit.example.ts.net"}}) {
		t.Fatalf("List() after remove = %v", servers)
	}
}

func TestAddRejectsDuplicatesAndInvalidServers(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if err := store.Add(Server{Name: "dev", Kind: KindHome, Home: "/tmp/a"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Add(Server{Name: "dev", Kind: KindHome, Home: "/tmp/b"}); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate Add() error = %v, want ErrExists", err)
	}
	for _, server := range []Server{
		{Name: "local", Kind: KindHome, Home: "/tmp/a"},
		{Name: "", Kind: KindHome, Home: "/tmp/a"},
		{Name: "Dev", Kind: KindHome, Home: "/tmp/a"},
		{Name: "-dev", Kind: KindHome, Home: "/tmp/a"},
		{Name: "my server", Kind: KindHome, Home: "/tmp/a"},
		{Name: strings.Repeat("a", 33), Kind: KindHome, Home: "/tmp/a"},
		{Name: "rel", Kind: KindHome, Home: "relative/home"},
		{Name: "unclean", Kind: KindHome, Home: "/tmp/../tmp/a"},
		{Name: "nokind", Home: "/tmp/a"},
		{Name: "both", Kind: KindHome, Home: "/tmp/a", URL: "https://a.example"},
		{Name: "path", Kind: KindURL, URL: "https://a.example/kit"},
		{Name: "upper", Kind: KindURL, URL: "https://A.example"},
		{Name: "scheme", Kind: KindURL, URL: "ftp://a.example"},
	} {
		if err := store.Add(server); err == nil {
			t.Errorf("Add(%+v) succeeded, want an error", server)
		}
	}
	if err := store.Add(Server{Name: "a.b-c_9", Kind: KindHome, Home: "/tmp/c"}); err != nil {
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
		"kind":      `{"servers": [{"name": "a", "kind": "ssh", "url": "https://a.example"}]}`,
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

func TestEntriesWithoutKindsAreHomes(t *testing.T) {
	t.Parallel()
	store, path := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"servers": [{"name": "dev", "home": "/tmp/kit-dev"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	servers, err := store.List()
	if err != nil || !reflect.DeepEqual(servers, []Server{{Name: "dev", Kind: KindHome, Home: "/tmp/kit-dev"}}) {
		t.Fatalf("List() = %+v, %v", servers, err)
	}
}

func TestCanonicalURL(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"https://Kit.Example.ts.net/": "https://kit.example.ts.net",
		" http://127.0.0.1:7777 ":     "http://127.0.0.1:7777",
	} {
		if got, err := CanonicalURL(input); err != nil || got != want {
			t.Errorf("CanonicalURL(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"kit.example", "https://kit.example/path", "https://kit.example/pair#abc", "https://u@kit.example"} {
		if _, err := CanonicalURL(input); err == nil {
			t.Errorf("CanonicalURL(%q) succeeded", input)
		}
	}
}

func TestTokensAreSavedPrivately(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "home", "server-tokens.json")
	tokens, err := NewTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	const serverURL = "https://kit.example.com"
	if _, err := tokens.Get("vps", serverURL); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() before Set error = %v", err)
	}
	token := "kit_device_" + strings.Repeat("A", 43)
	if err := tokens.Set("vps", "HTTPS://Kit.Example.com/", token); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got, err := tokens.Get("vps", serverURL); err != nil || got != token {
		t.Fatalf("Get() = %q, %v", got, err)
	}
	// A token is bound to the URL that issued it.
	if _, err := tokens.Get("vps", "https://other.example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() for another URL error = %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, %v", info.Mode().Perm(), err)
	}
	if err := tokens.Set("vps", serverURL, "not-a-token"); err == nil {
		t.Fatal("Set accepted a malformed token")
	}
	if err := tokens.Set("vps", "kit.example.com", token); err == nil {
		t.Fatal("Set accepted a URL that is not an origin")
	}
	if err := tokens.Delete("vps"); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Delete("vps"); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if _, err := tokens.Get("vps", serverURL); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after Delete error = %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.Get("vps", serverURL); err == nil {
		t.Fatal("a token file readable by others was accepted")
	}
}

func TestSSHServersAreSavedWithTheirDestination(t *testing.T) {
	t.Parallel()
	store, path := newTestStore(t)
	server := Server{Name: "devbox", Kind: KindSSH, Destination: "me@devbox.example.com"}
	if err := store.Add(server); err != nil {
		t.Fatal(err)
	}
	found, err := store.Find("devbox")
	if err != nil || found != server || found.Location() != "me@devbox.example.com" {
		t.Fatalf("Find() = %#v, %v", found, err)
	}
	data, _ := os.ReadFile(path)
	want := "{\n  \"servers\": [\n    {\n      \"name\": \"devbox\",\n      \"kind\": \"ssh\",\n      \"destination\": \"me@devbox.example.com\"\n    }\n  ]\n}\n"
	if string(data) != want {
		t.Fatalf("file = %q", data)
	}
	for _, invalid := range []Server{
		{Name: "opt", Kind: KindSSH, Destination: "-oProxyCommand=x"},
		{Name: "empty", Kind: KindSSH},
		{Name: "mixed", Kind: KindSSH, Destination: "devbox", URL: "https://kit.example.com"},
	} {
		if err := store.Add(invalid); err == nil {
			t.Fatalf("Add(%#v) accepted it", invalid)
		}
	}
}

// A newer Kit may save kinds this one does not know. They are not listed,
// keep their names, and survive changes unchanged.
func TestUnknownKindsAreKeptButNotListed(t *testing.T) {
	t.Parallel()
	store, path := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	future := `{"name":"later","kind":"carrier-pigeon","coop":"roof"}`
	if err := os.WriteFile(path, []byte(`{"servers":[`+future+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if servers, err := store.List(); err != nil || len(servers) != 0 {
		t.Fatalf("List() = %v, %v", servers, err)
	}
	if err := store.Add(Server{Name: "later", Kind: KindHome, Home: "/tmp/kit"}); !errors.Is(err, ErrExists) {
		t.Fatalf("Add() over an unknown entry = %v", err)
	}
	if _, err := store.Find("later"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Find() of an unknown entry = %v", err)
	}
	if err := store.Add(Server{Name: "dev", Kind: KindHome, Home: "/tmp/kit"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("dev"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var file struct{ Servers []map[string]string }
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if want := []map[string]string{{"name": "later", "kind": "carrier-pigeon", "coop": "roof"}}; !reflect.DeepEqual(file.Servers, want) {
		t.Fatalf("the unknown entry changed: %s", data)
	}
}

func TestUnknownEntriesCanBeRemovedAndNewFieldsSurvive(t *testing.T) {
	t.Parallel()
	store, path := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"servers":[{"name":"later","kind":"carrier-pigeon"},{"name":"dev","kind":"home","home":"/tmp/kit","color":"blue"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("later"); err != nil {
		t.Fatalf("Remove() of an unknown entry = %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"color": "blue"`) || strings.Contains(string(data), "later") {
		t.Fatalf("file = %s", data)
	}
}
