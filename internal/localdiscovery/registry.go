package localdiscovery

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/version"
)

// CredentialSource is the non-secret source used by a daemon provider.
type CredentialSource string

const (
	// CredentialSourceStore means the provider uses Kit's persistent credential store.
	CredentialSourceStore CredentialSource = "store"
	// CredentialSourceEnvironment means the provider uses environment credentials.
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
		if !validIdentifier(providerID) {
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
	if parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
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

// Load reads and validates the published local daemon registry.
func Load(paths apphome.Paths) (Registry, error) {
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

func loadToken(paths apphome.Paths) (string, error) {
	body, err := os.ReadFile(paths.ServerToken)
	if err != nil {
		return "", fmt.Errorf("read daemon token: %w", err)
	}
	token := strings.TrimSpace(string(body))
	if err := validateToken(token); err != nil {
		return "", err
	}
	return token, nil
}

func validateToken(token string) error {
	if len(token) != 64 {
		return errors.New("daemon token has an invalid length")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return errors.New("daemon token is not hexadecimal")
	}
	return nil
}

func validIdentifier(value string) bool {
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
