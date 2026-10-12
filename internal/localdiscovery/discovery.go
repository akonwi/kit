// Package localdiscovery owns the local daemon publication contract: the
// registry and token files a daemon publishes and clients discover. It does
// not manage daemon lifecycle.
package localdiscovery

import (
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
)

// Discover reads the published registry and token and returns the registry
// with the authenticated connection derived from that same publication.
func Discover(paths apphome.Paths) (Registry, clienttransport.Connection, error) {
	registry, err := Load(paths)
	if err != nil {
		return Registry{}, clienttransport.Connection{}, err
	}
	token, err := loadToken(paths)
	if err != nil {
		return Registry{}, clienttransport.Connection{}, err
	}
	return registry, clienttransport.Connection{
		URL: registry.URL, Token: token, InstanceID: registry.InstanceID,
		PID: registry.PID, ProtocolVersion: registry.ProtocolVersion,
		KitVersion: registry.KitVersion, VerifyPublishedIdentity: true,
	}, nil
}

// Resolve returns the authenticated connection to the published local daemon.
func Resolve(paths apphome.Paths) (clienttransport.Connection, error) {
	_, connection, err := Discover(paths)
	return connection, err
}
