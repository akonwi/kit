package updatebridge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/selfupdate"
)

func TestCheckAndInstallANewerRelease(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "kit")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\necho 'kit 0.48.0 (old)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := tarball(t, "#!/bin/sh\necho 'kit 0.49.0 (new)'\n")
	name := selfupdate.ArchiveName("v0.49.0", runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(archive)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/akonwi/kit/releases":
			_ = json.NewEncoder(writer).Encode([]map[string]any{{
				"tag_name": "v0.49.0",
				"body":     "# Kit 0.49.0\n\nKit tells you when an update is out.\n",
				"html_url": "https://github.com/akonwi/kit/releases/tag/v0.49.0",
				"assets": []map[string]any{{
					"name": name, "browser_download_url": server.URL + "/archive",
					"size": len(archive), "digest": "sha256:" + hex.EncodeToString(sum[:]),
				}},
			}})
		case "/archive":
			_, _ = writer.Write(archive)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	checker := selfupdate.Checker{APIURL: server.URL}
	locate := func() (string, error) { return executable, nil }

	update, err := check(context.Background(), checker, "0.48.0", locate)
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(executable)
	type shown struct {
		Available                               bool
		Current, Version, Summary, URL, Install string
	}
	want := shown{
		Available: true, Current: "0.48.0", Version: "0.49.0",
		Summary: "Kit tells you when an update is out.",
		URL:     "https://github.com/akonwi/kit/releases/tag/v0.49.0",
		Install: "replace " + resolved + " with " + name,
	}
	got := shown{Available: update.Available, Current: update.Current, Version: update.Version, Summary: update.Summary, URL: update.URL, Install: update.Install}
	if update.Launcher != resolved {
		t.Fatalf("Launcher = %q; want %q", update.Launcher, resolved)
	}
	if got != want {
		t.Fatalf("check() = %+v; want %+v", got, want)
	}

	var reports []int
	if err := install(context.Background(), selfupdate.Installer{}, update, func(percent int) { reports = append(reports, percent) }); err != nil {
		t.Fatal(err)
	}
	if len(reports) < 2 || reports[0] != -1 || reports[len(reports)-1] != 100 {
		t.Fatalf("progress = %v; want -1 first and 100 last", reports)
	}
	if installed, err := selfupdate.ExecutableVersion(context.Background(), executable); err != nil || installed.String() != "0.49.0" {
		t.Fatalf("installed = %v, %v", installed, err)
	}

	if update, err := check(context.Background(), checker, "0.49.0", locate); err != nil || update.Available {
		t.Fatalf("check(up to date) = %+v, %v", update, err)
	}
	if update, err := check(context.Background(), checker, "dev", locate); err != nil || update.Available {
		t.Fatalf("check(dev) = %+v, %v", update, err)
	}
	if err := install(context.Background(), selfupdate.Installer{}, Update{}, nil); err == nil {
		t.Fatal("installing no update succeeded")
	}
}

func TestRestartsOnlyAnOlderReleaseServer(t *testing.T) {
	cases := []struct {
		server string
		want   bool
	}{
		{"0.48.0", true},
		{"0.49.0", false},
		{"0.50.0", false},
		{"dev", false},
		{"", false},
	}
	for _, c := range cases {
		if got := Restarts(c.server, "0.49.0"); got != c.want {
			t.Errorf("Restarts(%q, 0.49.0) = %v; want %v", c.server, got, c.want)
		}
	}
}

func TestSettleWaitsForAnInstallToStop(t *testing.T) {
	beginInstall()
	released := make(chan struct{})
	go func() {
		<-released
		endInstall()
	}()
	var stderr bytes.Buffer
	settled := make(chan struct{})
	go func() {
		Settle(&stderr)
		close(settled)
	}()
	select {
	case <-settled:
		t.Fatal("Settle returned while an install ran")
	case <-time.After(50 * time.Millisecond):
	}
	close(released)
	<-settled
	if stderr.String() != "Stopping the update…\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}

	stderr.Reset()
	Settle(&stderr)
	if stderr.Len() != 0 {
		t.Fatalf("Settle without an install wrote %q", stderr.String())
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
