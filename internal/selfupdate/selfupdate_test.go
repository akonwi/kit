package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseVersionAcceptsReleaseVersionsOnly(t *testing.T) {
	for input, want := range map[string]Version{
		"0.48.0":   {0, 48, 0},
		"v1.2.3":   {1, 2, 3},
		"10.0.12":  {10, 0, 12},
		"v0.0.0":   {0, 0, 0},
		"2.30.400": {2, 30, 400},
	} {
		got, ok := ParseVersion(input)
		if !ok || got != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", input, got, ok, want)
		}
	}
	for _, input := range []string{"dev", "", "1.2", "1.2.3-rc.1", "01.2.3", "vv1.2.3", "1.2.3.4", " 1.2.3"} {
		if got, ok := ParseVersion(input); ok {
			t.Errorf("ParseVersion(%q) = %v; want no release version", input, got)
		}
	}
}

func TestVersionsCompareNumerically(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.9.0", "0.10.0", -1},
		{"0.10.0", "0.9.9", 1},
		{"1.0.0", "0.99.99", 1},
		{"0.48.1", "0.48.0", 1},
		{"0.48.0", "0.48.0", 0},
	}
	for _, c := range cases {
		a, _ := ParseVersion(c.a)
		b, _ := ParseVersion(c.b)
		if got := a.Compare(b); got != c.want {
			t.Errorf("%s.Compare(%s) = %d; want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCurrentRejectsDevelopmentAndChannelBuilds(t *testing.T) {
	if got, err := Current("0.48.0", ""); err != nil || got != (Version{0, 48, 0}) {
		t.Fatalf("Current(release) = %v, %v", got, err)
	}
	if _, err := Current("dev", ""); err != ErrNotRelease {
		t.Fatalf("Current(dev) error = %v; want ErrNotRelease", err)
	}
	if _, err := Current("0.48.0", "dev"); err != ErrNotRelease {
		t.Fatalf("Current(channel build) error = %v; want ErrNotRelease", err)
	}
}

func TestCurrentReleaseURL(t *testing.T) {
	cases := []struct {
		artifact         Artifact
		version, channel string
		want             string
	}{
		{ArtifactCLI, "0.48.0", "", "https://github.com/akonwi/kit/releases/tag/v0.48.0"},
		{ArtifactMacOS, "0.4.0", "", "https://github.com/akonwi/kit/releases/tag/macos-v0.4.0"},
		{ArtifactCLI, "dev", "", "https://github.com/akonwi/kit/releases"},
		{ArtifactCLI, "0.48.0", "dev", "https://github.com/akonwi/kit/releases"},
	}
	for _, c := range cases {
		if got := CurrentReleaseURL(c.artifact, c.version, c.channel); got != c.want {
			t.Errorf("CurrentReleaseURL(%q, %q, %q) = %q; want %q", c.artifact, c.version, c.channel, got, c.want)
		}
	}
}

func TestSummaryIsTheFirstParagraphAfterTheTitle(t *testing.T) {
	release := Release{Notes: "# Kit 0.49.0\r\n\r\nKit tells you when an update is out,\r\nand `kit update` installs it.\r\n\r\n## What's changed\r\n\r\n- one\n"}
	want := "Kit tells you when an update is out, and `kit update` installs it."
	if got := release.Summary(); got != want {
		t.Fatalf("Summary() = %q; want %q", got, want)
	}
}

func TestSummaryReplacesControlCharacters(t *testing.T) {
	release := Release{Notes: "# Kit\n\nSafe \x1b]8;;https://example.invalid\x07text\x1b[2J."}
	want := "Safe  ]8;;https://example.invalid text [2J."
	if got := release.Summary(); got != want {
		t.Fatalf("Summary() = %q; want %q", got, want)
	}
}

func releaseJSON(tag string, draft, prerelease bool, assets ...githubAsset) githubRelease {
	return githubRelease{TagName: tag, Body: "# " + tag, Draft: draft, Prerelease: prerelease, Assets: assets}
}

func serveReleases(t *testing.T, releases []githubRelease, files map[string][]byte) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/repos/akonwi/kit/releases":
			if request.URL.Query().Get("per_page") == "" {
				t.Errorf("release list requested without per_page")
			}
			_ = json.NewEncoder(writer).Encode(releases)
		case strings.HasPrefix(request.URL.Path, "/download/"):
			content, ok := files[strings.TrimPrefix(request.URL.Path, "/download/")]
			if !ok {
				http.NotFound(writer, request)
				return
			}
			_, _ = writer.Write(content)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestLatestChoosesTheNewestPublishedReleaseOfTheArtifact(t *testing.T) {
	server := serveReleases(t, []githubRelease{
		releaseJSON("macos-v0.9.0", false, false),
		releaseJSON("v0.50.0-rc.1", false, true),
		releaseJSON("v0.51.0", true, false),
		releaseJSON("v0.9.0", false, false),
		releaseJSON("v0.49.0", false, false, githubAsset{Name: "kit_v0.49.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.test/a", Size: 3, Digest: "sha256:ABCDEF"}),
		releaseJSON("v0.48.0", false, false),
		releaseJSON("macos-v0.4.0", false, false),
	}, nil)
	checker := Checker{APIURL: server.URL}

	latest, err := checker.Latest(context.Background(), ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Tag != "v0.49.0" || latest.Version != (Version{0, 49, 0}) || latest.URL != "https://github.com/akonwi/kit/releases/tag/v0.49.0" {
		t.Fatalf("Latest(cli) = %+v", latest)
	}
	asset, ok := latest.Asset("kit_v0.49.0_linux_amd64.tar.gz")
	if !ok || asset.SHA256 != "abcdef" || asset.Size != 3 || asset.URL != "https://example.test/a" {
		t.Fatalf("asset = %+v, %v", asset, ok)
	}

	app, err := checker.Latest(context.Background(), ArtifactMacOS)
	if err != nil {
		t.Fatal(err)
	}
	if app.Tag != "macos-v0.9.0" {
		t.Fatalf("Latest(macOS) = %s; want macos-v0.9.0", app.Tag)
	}

	newer, ok, err := checker.Newer(context.Background(), ArtifactCLI, Version{0, 48, 0})
	if err != nil || !ok || newer.Tag != "v0.49.0" {
		t.Fatalf("Newer(0.48.0) = %s, %v, %v", newer.Tag, ok, err)
	}
	if _, ok, err := checker.Newer(context.Background(), ArtifactCLI, Version{0, 49, 0}); err != nil || ok {
		t.Fatalf("Newer(0.49.0) = %v, %v; want up to date", ok, err)
	}
}

func TestLatestReportsAFailedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()
	_, err := Checker{APIURL: server.URL}.Latest(context.Background(), ArtifactCLI)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("Latest() error = %v; want the 403 reported", err)
	}
}

func TestLocateRecognizesHomebrewCellar(t *testing.T) {
	prefix := t.TempDir()
	cellar := filepath.Join(prefix, "Cellar", "kit", "0.48.0", "bin")
	bin := filepath.Join(prefix, "bin")
	for _, directory := range []string{cellar, bin} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(cellar, "kit")
	writeScript(t, executable, "echo kit 0.48.0")
	writeScript(t, filepath.Join(bin, "brew"), "exit 0")
	if err := os.Symlink(executable, filepath.Join(bin, "kit")); err != nil {
		t.Fatal(err)
	}

	installation, err := Locate(filepath.Join(bin, "kit"))
	if err != nil {
		t.Fatal(err)
	}
	resolvedPrefix, _ := filepath.EvalSymlinks(prefix)
	want := Installation{
		Method:     MethodHomebrew,
		Executable: filepath.Join(resolvedPrefix, "Cellar", "kit", "0.48.0", "bin", "kit"),
		Launcher:   filepath.Join(resolvedPrefix, "opt", "kit", "bin", "kit"),
		Brew:       filepath.Join(resolvedPrefix, "bin", "brew"),
	}
	if installation != want {
		t.Fatalf("Locate() = %+v; want %+v", installation, want)
	}
	if got := installation.Describe(Release{Tag: "v0.49.0"}); got != "brew upgrade akonwi/tap/kit" {
		t.Fatalf("Describe() = %q", got)
	}

	if err := os.Remove(filepath.Join(bin, "brew")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	installation, err = Locate(filepath.Join(bin, "kit"))
	if err != nil {
		t.Fatal(err)
	}
	if installation.Brew != "" {
		t.Fatalf("Locate() found brew %q; want none", installation.Brew)
	}
	if err := (Installer{}).Install(context.Background(), installation, Release{Tag: "v0.49.0"}, nil); err == nil || err.Error() != "Homebrew's brew command was not found" {
		t.Fatalf("Install() error = %v; want brew reported missing", err)
	}
}

func TestHomebrewInstallRunsBrewAndChecksTheInstalledVersion(t *testing.T) {
	prefix := t.TempDir()
	launcher := filepath.Join(prefix, "kit")
	log := filepath.Join(prefix, "brew.log")
	writeScript(t, launcher, "echo 'kit 0.48.0 (old)'")
	brew := filepath.Join(prefix, "brew")
	writeScript(t, brew, "echo \"$@\" > '"+log+"'\nprintf '#!/bin/sh\\necho \"kit 0.49.0 (new)\"\\n' > '"+launcher+"'")
	installation := Installation{Method: MethodHomebrew, Executable: launcher, Launcher: launcher, Brew: brew}

	var output bytes.Buffer
	err := Installer{Output: &output}.Install(context.Background(), installation, Release{Tag: "v0.49.0", Version: Version{0, 49, 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	arguments, _ := os.ReadFile(log)
	if string(arguments) != "upgrade akonwi/tap/kit\n" {
		t.Fatalf("brew ran with %q", arguments)
	}
}

func TestHomebrewInstallReportsAnUnchangedVersionAndBrewFailures(t *testing.T) {
	prefix := t.TempDir()
	launcher := filepath.Join(prefix, "kit")
	writeScript(t, launcher, "echo 'kit 0.48.0 (old)'")
	brew := filepath.Join(prefix, "brew")
	writeScript(t, brew, "echo 'Warning: akonwi/tap/kit 0.48.0 already installed'")
	installation := Installation{Method: MethodHomebrew, Executable: launcher, Launcher: launcher, Brew: brew}
	release := Release{Tag: "v0.49.0", Version: Version{0, 49, 0}}

	err := Installer{}.Install(context.Background(), installation, release, nil)
	if err == nil || err.Error() != "Homebrew installed 0.48.0, not 0.49.0; its tap may not have the release yet" {
		t.Fatalf("Install() error = %v", err)
	}

	writeScript(t, brew, "echo 'Updating Homebrew...'\necho 'Error: no space left on device' >&2\nexit 1")
	err = Installer{}.Install(context.Background(), installation, release, nil)
	if err == nil || err.Error() != "brew upgrade failed: Error: no space left on device" {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestCancelledHomebrewInstallInterruptsBrewAndWaitsForIt(t *testing.T) {
	prefix := t.TempDir()
	launcher := filepath.Join(prefix, "kit")
	writeScript(t, launcher, "echo 'kit 0.48.0 (old)'")
	started := filepath.Join(prefix, "started")
	rolledBack := filepath.Join(prefix, "rolled-back")
	brew := filepath.Join(prefix, "brew")
	// Homebrew rolls an interrupted upgrade back before exiting.
	writeScript(t, brew, "trap 'sleep 0.2; touch \""+rolledBack+"\"; exit 130' INT\ntouch \""+started+"\"\nwhile :; do sleep 0.05; done")
	installation := Installation{Method: MethodHomebrew, Executable: launcher, Launcher: launcher, Brew: brew}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			if _, err := os.Stat(started); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	err := Installer{}.Install(ctx, installation, Release{Tag: "v0.49.0", Version: Version{0, 49, 0}}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Install() error = %v; want cancellation", err)
	}
	if _, err := os.Stat(rolledBack); err != nil {
		t.Fatalf("brew did not finish handling the interrupt: %v", err)
	}
}

func TestArchiveInstallRemovesStagingLeftByAKilledUpdate(t *testing.T) {
	directory := t.TempDir()
	stale := filepath.Join(directory, ".kit-update-111")
	fresh := filepath.Join(directory, ".kit-update-222")
	for _, path := range []string{stale, fresh} {
		if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	removeStaleStaging(directory, time.Now())

	assertOnlyFiles(t, directory, ".kit-update-222")
}

func TestArchiveInstallVerifiesAndReplacesTheExecutable(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "kit")
	writeScript(t, executable, "echo 'kit 0.48.0 (old)'")
	archive := tarball(t, "#!/bin/sh\necho 'kit 0.49.0 (new)'\n")
	name := ArchiveName("v0.49.0", runtime.GOOS, runtime.GOARCH)
	server := serveReleases(t, nil, map[string][]byte{name: archive})
	release := Release{Tag: "v0.49.0", Version: Version{0, 49, 0}, Assets: []Asset{{
		Name: name, URL: server.URL + "/download/" + name, Size: int64(len(archive)), SHA256: digest(archive),
	}}}

	var reports [][2]int64
	err := Installer{}.Install(context.Background(), Installation{Method: MethodArchive, Executable: executable, Launcher: executable}, release, func(received, total int64) {
		reports = append(reports, [2]int64{received, total})
	})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := ExecutableVersion(context.Background(), executable)
	if err != nil || installed != (Version{0, 49, 0}) {
		t.Fatalf("installed version = %v, %v", installed, err)
	}
	if len(reports) == 0 || reports[len(reports)-1] != [2]int64{int64(len(archive)), int64(len(archive))} {
		t.Fatalf("progress reports = %v", reports)
	}
	assertOnlyFiles(t, directory, "kit")
}

func TestArchiveInstallKeepsTheExecutableWhenVerificationFails(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "kit")
	writeScript(t, executable, "echo 'kit 0.48.0 (old)'")
	name := ArchiveName("v0.49.0", runtime.GOOS, runtime.GOARCH)
	good := tarball(t, "#!/bin/sh\necho 'kit 0.49.0 (new)'\n")
	wrongVersion := tarball(t, "#!/bin/sh\necho 'kit 0.47.0 (new)'\n")
	installation := Installation{Method: MethodArchive, Executable: executable, Launcher: executable}

	cases := map[string]struct {
		archive []byte
		digest  string
		want    string
	}{
		"digest mismatch": {good, digest(wrongVersion), name + " does not match its SHA-256 digest on GitHub"},
		"wrong version":   {wrongVersion, digest(wrongVersion), "downloaded executable reports 0.47.0, not 0.49.0"},
		"no digest":       {good, "", "GitHub reports no SHA-256 digest for " + name},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			server := serveReleases(t, nil, map[string][]byte{name: c.archive})
			release := Release{Tag: "v0.49.0", Version: Version{0, 49, 0}, Assets: []Asset{{
				Name: name, URL: server.URL + "/download/" + name, Size: int64(len(c.archive)), SHA256: c.digest,
			}}}
			err := Installer{}.Install(context.Background(), installation, release, nil)
			if err == nil || err.Error() != c.want {
				t.Fatalf("Install() error = %v; want %q", err, c.want)
			}
			installed, err := ExecutableVersion(context.Background(), executable)
			if err != nil || installed != (Version{0, 48, 0}) {
				t.Fatalf("executable after failure = %v, %v", installed, err)
			}
			assertOnlyFiles(t, directory, "kit")
		})
	}

	release := Release{Tag: "v0.49.0", Version: Version{0, 49, 0}}
	err := Installer{}.Install(context.Background(), installation, release, nil)
	if err == nil || err.Error() != "release v0.49.0 has no "+name {
		t.Fatalf("Install() without an archive error = %v", err)
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func tarball(t *testing.T, executable string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	if err := archive.WriteHeader(&tar.Header{Name: "kit", Mode: 0o755, Size: int64(len(executable)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write([]byte(executable)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func assertOnlyFiles(t *testing.T, directory string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("%s holds %v; want %v", directory, names, want)
	}
}
