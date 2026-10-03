// Package localdiscovery reads the authenticated local daemon connection
// without managing its lifecycle.
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

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/version"
)

type registry struct {
	RegistryVersion   int               `json:"registryVersion"`
	ProtocolVersion   int               `json:"protocolVersion"`
	KitVersion        string            `json:"kitVersion"`
	PID               int               `json:"pid"`
	InstanceID        string            `json:"instanceId"`
	URL               string            `json:"url"`
	CredentialSources map[string]string `json:"credentialSources,omitempty"`
}

// Resolve reads and validates the current local daemon registry and token.
func Resolve(paths apphome.Paths) (clienttransport.Connection, error) {
	body, err := os.ReadFile(paths.ServerRegistry)
	if err != nil {
		return clienttransport.Connection{}, fmt.Errorf("read daemon registry: %w", err)
	}
	var record registry
	if err := json.Unmarshal(body, &record); err != nil {
		return clienttransport.Connection{}, fmt.Errorf("decode daemon registry: %w", err)
	}
	if record.RegistryVersion != version.LocalRegistryVersion || record.ProtocolVersion < 1 || record.PID < 1 || record.InstanceID == "" {
		return clienttransport.Connection{}, errors.New("daemon registry identity is invalid")
	}
	for providerID, source := range record.CredentialSources {
		if !validIdentifier(providerID) || source != "store" && source != "environment" {
			return clienttransport.Connection{}, fmt.Errorf("invalid credential source for provider %q", providerID)
		}
	}
	parsed, err := url.Parse(record.URL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return clienttransport.Connection{}, fmt.Errorf("invalid daemon URL %q", record.URL)
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return clienttransport.Connection{}, fmt.Errorf("invalid daemon URL host %q: %w", parsed.Host, err)
	}
	address := net.ParseIP(host)
	if address == nil || !address.IsLoopback() {
		return clienttransport.Connection{}, fmt.Errorf("daemon URL must use a loopback IP, got %q", host)
	}

	tokenBody, err := os.ReadFile(paths.ServerToken)
	if err != nil {
		return clienttransport.Connection{}, fmt.Errorf("read daemon token: %w", err)
	}
	token := strings.TrimSpace(string(tokenBody))
	if len(token) != 64 {
		return clienttransport.Connection{}, errors.New("daemon token has an invalid length")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return clienttransport.Connection{}, errors.New("daemon token is not hexadecimal")
	}
	return clienttransport.Connection{
		URL: record.URL, Token: token, InstanceID: record.InstanceID,
		PID: record.PID, ProtocolVersion: record.ProtocolVersion,
		KitVersion: record.KitVersion, VerifyPublishedIdentity: true,
	}, nil
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
