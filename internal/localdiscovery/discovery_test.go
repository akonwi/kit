package localdiscovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/version"
)

func TestResolveReturnsValidatedReadOnlyConnection(t *testing.T) {
	paths := apphome.FromHome(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.ServerRegistry), 0o700); err != nil {
		t.Fatal(err)
	}
	record := registry{
		RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: version.SessionProtocolVersion,
		KitVersion: version.Version, PID: 42, InstanceID: "instance_test", URL: "http://127.0.0.1:4321",
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ServerRegistry, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ServerToken, []byte(strings.Repeat("a", 64)), 0o600); err != nil {
		t.Fatal(err)
	}

	connection, err := Resolve(paths)
	if err != nil {
		t.Fatal(err)
	}
	if connection.URL != record.URL || connection.InstanceID != record.InstanceID || connection.Token != strings.Repeat("a", 64) || !connection.VerifyPublishedIdentity {
		t.Fatalf("connection = %+v", connection)
	}
}

func TestResolveRejectsNonLoopbackRegistry(t *testing.T) {
	paths := apphome.FromHome(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.ServerRegistry), 0o700); err != nil {
		t.Fatal(err)
	}
	record := registry{
		RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: version.SessionProtocolVersion,
		KitVersion: version.Version, PID: 42, InstanceID: "instance_test", URL: "http://example.com:4321",
	}
	body, _ := json.Marshal(record)
	if err := os.WriteFile(paths.ServerRegistry, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ServerToken, []byte(strings.Repeat("a", 64)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(paths); err == nil {
		t.Fatal("Resolve accepted a non-loopback local daemon")
	}
}
