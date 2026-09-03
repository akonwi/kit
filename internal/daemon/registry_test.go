package daemon

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/version"
)

func TestRegistryValidatesCredentialSources(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	valid := Registry{
		RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: version.SessionProtocolVersion,
		KitVersion: "dev", PID: 1, InstanceID: "instance", URL: "http://" + address,
		StartedAt: time.Now(), CredentialSources: map[string]CredentialSource{"openai-codex": CredentialSourceStore},
	}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.CredentialSources = map[string]CredentialSource{"openai-codex": "unknown"}
	if err := invalid.validate(); err == nil {
		t.Fatal("Registry.validate() accepted an unknown credential source")
	}
	invalid.CredentialSources = map[string]CredentialSource{"bad\nprovider": CredentialSourceStore}
	if err := invalid.validate(); err == nil {
		t.Fatal("Registry.validate() accepted an unsafe provider id")
	}
}

func TestWritePrivateFileDoesNotReplacePublishedDestination(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state")
	if err := writePrivateFile(path, []byte("first")); err != nil {
		t.Fatalf("first writePrivateFile() error = %v", err)
	}
	if err := writePrivateFile(path, []byte("second")); err == nil {
		t.Fatal("writePrivateFile() replaced an existing publication")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "first" {
		t.Fatalf("body = %q, want first publication", body)
	}
}

func TestWritePrivateFilePublishesExactlyOneConcurrentWriter(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state")
	var successes atomic.Int32
	var group sync.WaitGroup
	for _, body := range []string{"first", "second"} {
		group.Add(1)
		go func() {
			defer group.Done()
			if writePrivateFile(path, []byte(body)) == nil {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful publications = %d, want 1", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "first" && string(body) != "second" {
		t.Fatalf("published partial or unexpected body %q", body)
	}
}

func TestClearRegistrationRemovesRegistryBeforeNextPublication(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := writePrivateFile(paths.ServerToken, []byte("old")); err != nil {
		t.Fatalf("write token: %v", err)
	}
	if err := writePrivateFile(paths.ServerRegistry, []byte("old")); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if err := clearRegistration(paths); err != nil {
		t.Fatalf("clearRegistration() error = %v", err)
	}
	if err := writePrivateFile(paths.ServerToken, []byte("new")); err != nil {
		t.Fatalf("publish new token: %v", err)
	}
	if err := writePrivateFile(paths.ServerRegistry, []byte("new")); err != nil {
		t.Fatalf("publish new registry: %v", err)
	}
}
