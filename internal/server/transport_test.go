package server

import (
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/localdiscovery"
)

// localTransport returns a session transport for the daemon published under
// paths, as the public API's local target resolves it.
func localTransport(paths apphome.Paths) *clienttransport.Client {
	return clienttransport.New(func() (clienttransport.Connection, error) {
		return localdiscovery.Resolve(paths)
	})
}
