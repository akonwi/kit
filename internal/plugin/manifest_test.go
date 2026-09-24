package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

func manifestJSON(id string) string {
	return `{"manifestVersion":1,"id":"` + id + `","transport":{"type":"stdio","command":"python3","args":["-u","plugin.py"]}}`
}

func TestParseManifest(t *testing.T) {
	valid := manifestJSON("speech")
	manifest, err := ParseManifest([]byte(valid), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Manifest{ManifestVersion: 1, ID: "speech", Transport: StdioTransport{Type: "stdio", Command: "python3", Args: []string{"-u", "plugin.py"}}}
	if !reflect.DeepEqual(manifest, want) {
		t.Fatalf("got %#v, want %#v", manifest, want)
	}
	for name, value := range map[string]string{
		"unknown field":                strings.Replace(valid, `"id":`, `"extra":true,"id":`, 1),
		"unknown transport field":      strings.Replace(valid, `"type":`, `"extra":true,"type":`, 1),
		"wrong field casing":           strings.Replace(valid, `"manifestVersion"`, `"ManifestVersion"`, 1),
		"wrong transport field casing": strings.Replace(valid, `"command"`, `"Command"`, 1),
		"wrong version":                strings.Replace(valid, `"manifestVersion":1`, `"manifestVersion":2`, 1),
		"missing version":              strings.Replace(valid, `"manifestVersion":1,`, ``, 1),
		"reserved kit":                 manifestJSON("kit"),
		"reserved prefix":              manifestJSON("kit-speech"),
		"uppercase id":                 manifestJSON("Speech"),
		"long id":                      manifestJSON(strings.Repeat("a", 33)),
		"wrong transport":              strings.Replace(valid, `"stdio"`, `"http"`, 1),
		"empty command":                strings.Replace(valid, `"python3"`, `""`, 1),
		"null command":                 strings.Replace(valid, `"python3"`, `null`, 1),
		"nul command":                  strings.Replace(valid, `"python3"`, `"py\u0000thon"`, 1),
		"null args":                    strings.Replace(valid, `["-u","plugin.py"]`, `null`, 1),
		"null arg":                     strings.Replace(valid, `"plugin.py"`, `null`, 1),
		"numeric arg":                  strings.Replace(valid, `"plugin.py"`, `12`, 1),
		"nul arg":                      strings.Replace(valid, `"plugin.py"`, `"\u0000"`, 1),
		"null name":                    strings.Replace(valid, `"id":`, `"name":null,"id":`, 1),
		"empty name":                   strings.Replace(valid, `"id":`, `"name":"","id":`, 1),
		"long name":                    strings.Replace(valid, `"id":`, `"name":"`+strings.Repeat("a", 129)+`","id":`, 1),
		"invalid schema":               strings.Replace(valid, `"id":`, `"$schema":"relative/path","id":`, 1),
		"multiple documents":           valid + valid,
		"null":                         "null",
		"array":                        "[]",
		"invalid UTF-8":                string([]byte{0xff}),
		"too large":                    strings.Repeat(" ", MaxManifestBytes) + valid,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(value), nil); err == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
	if _, err := ParseManifest([]byte(valid), []string{"speech"}); err == nil {
		t.Fatal("accepted reserved command domain")
	}
	unicodeName := strings.Replace(valid, `"id":`, `"name":"`+strings.Repeat("猫", 128)+`","id":`, 1)
	if _, err := ParseManifest([]byte(unicodeName), nil); err != nil {
		t.Fatalf("Unicode character count: %v", err)
	}
	minimal := `{"manifestVersion":1,"id":"minimal","transport":{"type":"stdio","command":"./run"}}`
	if _, err := ParseManifest([]byte(minimal), nil); err != nil {
		t.Fatal(err)
	}
}

func writeInstallation(t *testing.T, root, name, document string) string {
	t.Helper()
	path := filepath.Join(root, name, "plugin.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverOrderPrecedenceAndIsolation(t *testing.T) {
	home := apphome.FromHome(filepath.Join(t.TempDir(), "v2"))
	cwd := t.TempDir()
	project := filepath.Join(cwd, ".kit", "plugins")
	first := writeInstallation(t, home.Plugins, "a-owner", manifestJSON("shared"))
	writeInstallation(t, home.Plugins, "z-user", manifestJSON("user-only"))
	duplicate := writeInstallation(t, home.Plugins, "b-duplicate", manifestJSON("shared"))
	projectDuplicate := writeInstallation(t, project, "a-duplicate", manifestJSON("shared"))
	invalid := writeInstallation(t, project, "b-invalid", `{"manifestVersion":2}`)
	writeInstallation(t, project, "c-good", manifestJSON("project-only"))
	writeInstallation(t, project, "nested/deeper", manifestJSON("too-deep"))
	// Explicit v2 paths must not silently include the legacy home, even if HOME
	// points at a directory containing a perfectly valid legacy installation.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	writeInstallation(t, filepath.Join(fakeHome, ".kit", "plugins"), "legacy", manifestJSON("legacy"))
	result, err := Discover(t.Context(), Discovery{Home: home, Cwd: cwd, IncludeUser: true, IncludeProject: true})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	var sources []Source
	for _, install := range result.Installations {
		ids = append(ids, install.Manifest.ID)
		sources = append(sources, install.Source)
	}
	if !reflect.DeepEqual(ids, []string{"shared", "user-only", "project-only"}) {
		t.Fatalf("ids = %v", ids)
	}
	if !reflect.DeepEqual(sources, []Source{User, User, Project}) {
		t.Fatalf("sources = %v", sources)
	}
	if len(result.Diagnostics) != 3 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	for i, path := range []string{duplicate, projectDuplicate} {
		d := result.Diagnostics[i]
		if d.Phase != "duplicate" || d.PluginID != "shared" || d.ManifestPath != path || d.OtherManifestPath != first {
			t.Fatalf("duplicate %d = %#v", i, d)
		}
	}
	if d := result.Diagnostics[2]; d.Phase != "manifest" || d.ManifestPath != invalid || d.Message == "" {
		t.Fatalf("invalid = %#v", d)
	}
}

func TestDiscoverSymlinksAndRetainedOwners(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	repo := t.TempDir()
	target := writeInstallation(t, repo, "standalone", manifestJSON("linked"))
	if err := os.MkdirAll(home.Plugins, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(target), filepath.Join(home.Plugins, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "missing"), filepath.Join(home.Plugins, "broken")); err != nil {
		t.Fatal(err)
	}
	result, err := Discover(t.Context(), Discovery{Home: home, IncludeUser: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Installations) != 1 || result.Installations[0].Name != "link" || result.Installations[0].Root != filepath.Join(home.Plugins, "link") {
		t.Fatalf("result = %#v", result)
	}
	cwd := t.TempDir()
	path := writeInstallation(t, filepath.Join(cwd, ".kit", "plugins"), "same", manifestJSON("linked"))
	next, err := Discover(t.Context(), Discovery{Cwd: cwd, IncludeProject: true, Existing: result.Installations})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Installations) != 0 || len(next.Diagnostics) != 1 || next.Diagnostics[0].ManifestPath != path || next.Diagnostics[0].OtherManifestPath != result.Installations[0].ManifestPath {
		t.Fatalf("replacement = %#v", next)
	}
}

func TestDiscoverMissingRootsCancellationAndInvalidPaths(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	result, err := Discover(t.Context(), Discovery{Home: home, Cwd: t.TempDir(), IncludeUser: true, IncludeProject: true})
	if err != nil || len(result.Installations) != 0 || len(result.Diagnostics) != 0 {
		t.Fatalf("missing roots = %#v, %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Discover(ctx, Discovery{Home: home, IncludeUser: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	for _, opts := range []Discovery{{IncludeUser: true}, {IncludeProject: true, Cwd: "relative"}} {
		if _, err := Discover(t.Context(), opts); err == nil {
			t.Fatal("accepted nonabsolute discovery root")
		}
	}
	if err := os.WriteFile(home.Plugins, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = Discover(t.Context(), Discovery{Home: home, IncludeUser: true})
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].ManifestPath != home.Plugins {
		t.Fatalf("root error = %#v, %v", result, err)
	}
}

func TestExistingV1ExampleManifests(t *testing.T) {
	for _, id := range []string{"plugin-demo", "speech", "ui-api-demo"} {
		t.Run(id, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", ".kit", "plugins", id, "plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := ParseManifest(data, nil)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.ID != id {
				t.Fatalf("id = %q", manifest.ID)
			}
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			roundtrip, err := ParseManifest(encoded, nil)
			if err != nil || !reflect.DeepEqual(roundtrip, manifest) {
				t.Fatalf("roundtrip = %#v, %v", roundtrip, err)
			}
		})
	}
}

func TestManifestVersionNumericEquality(t *testing.T) {
	for _, version := range []string{"1", "1.0", "1.000", "10e-1", "0.01e2", "1E+00"} {
		manifest, err := ParseManifest([]byte(strings.Replace(manifestJSON("numeric"), `"manifestVersion":1`, `"manifestVersion":`+version, 1)), nil)
		if err != nil || manifest.ManifestVersion != 1 {
			t.Errorf("version %s = %#v, %v", version, manifest, err)
		}
	}
	for _, version := range []string{`"1"`, "1.00000000000000001", "1e999999999999", "0", "10", "0.1", "-1", "1e-1"} {
		if _, err := ParseManifest([]byte(strings.Replace(manifestJSON("numeric"), `"manifestVersion":1`, `"manifestVersion":`+version, 1)), nil); err == nil {
			t.Errorf("accepted version %s", version)
		}
	}
}

func TestDiscoverReportsSymlinkFailureAndReservedDomain(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	path := writeInstallation(t, home.Plugins, "reserved", manifestJSON("session"))
	loop := filepath.Join(home.Plugins, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	result, err := Discover(t.Context(), Discovery{Home: home, IncludeUser: true, ReservedIDs: []string{"session"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Installations) != 0 || len(result.Diagnostics) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Diagnostics[0].ManifestPath != filepath.Join(loop, "plugin.json") || result.Diagnostics[1].ManifestPath != path {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
}
