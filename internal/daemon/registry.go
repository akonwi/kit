package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/securefs"
	"github.com/akonwi/kit/internal/version"
)

// CredentialSource is the non-secret source used by a daemon provider.
type CredentialSource string

const (
	CredentialSourceStore       CredentialSource = "store"
	CredentialSourceEnvironment CredentialSource = "environment"
)

// Registry is the public, non-secret local daemon discovery record.
type Registry struct {
	RegistryVersion   int                         `json:"registryVersion"`
	ProtocolVersion   int                         `json:"protocolVersion"`
	KitVersion        string                      `json:"kitVersion"`
	Commit            string                      `json:"commit"`
	PID               int                         `json:"pid"`
	InstanceID        string                      `json:"instanceId"`
	URL               string                      `json:"url"`
	StartedAt         time.Time                   `json:"startedAt"`
	CredentialSources map[string]CredentialSource `json:"credentialSources,omitempty"`
}

func (r Registry) validate() error {
	if r.RegistryVersion != version.LocalRegistryVersion {
		return fmt.Errorf("unsupported registry version %d", r.RegistryVersion)
	}
	if r.ProtocolVersion < 1 {
		return fmt.Errorf("invalid protocol version %d", r.ProtocolVersion)
	}
	if r.PID < 1 {
		return fmt.Errorf("invalid daemon pid %d", r.PID)
	}
	if r.InstanceID == "" {
		return errors.New("daemon instance id is empty")
	}
	for providerID, source := range r.CredentialSources {
		if !validRegistryIdentifier(providerID) {
			return fmt.Errorf("invalid credential source provider id %q", providerID)
		}
		switch source {
		case CredentialSourceStore, CredentialSourceEnvironment:
		default:
			return fmt.Errorf("invalid credential source %q for provider %q", source, providerID)
		}
	}
	parsed, err := url.Parse(r.URL)
	if err != nil {
		return fmt.Errorf("parse daemon URL: %w", err)
	}
	if parsed.Scheme != "http" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid daemon URL %q", r.URL)
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return fmt.Errorf("invalid daemon URL host %q: %w", parsed.Host, err)
	}
	address := net.ParseIP(host)
	if address == nil || !address.IsLoopback() {
		return fmt.Errorf("daemon URL must use a loopback IP, got %q", host)
	}
	return nil
}

// LoadRegistry reads and validates local daemon discovery metadata.
func LoadRegistry(paths apphome.Paths) (Registry, error) {
	body, err := os.ReadFile(paths.ServerRegistry)
	if err != nil {
		return Registry{}, fmt.Errorf("read daemon registry: %w", err)
	}
	var registry Registry
	if err := json.Unmarshal(body, &registry); err != nil {
		return Registry{}, fmt.Errorf("decode daemon registry: %w", err)
	}
	if err := registry.validate(); err != nil {
		return Registry{}, fmt.Errorf("validate daemon registry: %w", err)
	}
	return registry, nil
}

func writeRegistry(paths apphome.Paths, registry Registry) error {
	if err := registry.validate(); err != nil {
		return err
	}
	body, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode daemon registry: %w", err)
	}
	body = append(body, '\n')
	return writePrivateFile(paths.ServerRegistry, body)
}

func newToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate daemon token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func newInstanceID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate daemon instance id: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func loadToken(paths apphome.Paths) (string, error) {
	body, err := os.ReadFile(paths.ServerToken)
	if err != nil {
		return "", fmt.Errorf("read daemon token: %w", err)
	}
	token := strings.TrimSpace(string(body))
	if len(token) != 64 {
		return "", errors.New("daemon token has an invalid length")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", errors.New("daemon token is not hexadecimal")
	}
	return token, nil
}

func validRegistryIdentifier(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func writePrivateFile(path string, body []byte) error {
	if err := securefs.MakePrivateDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("create private file directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".kit-write-*")
	if err != nil {
		return fmt.Errorf("create temporary private file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := securefs.ProtectFile(temporaryPath); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary private file: %w", err)
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary private file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary private file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary private file: %w", err)
	}
	if err := publishNoReplace(temporaryPath, path); err != nil {
		return fmt.Errorf("publish private file %q: %w", path, err)
	}
	return nil
}
