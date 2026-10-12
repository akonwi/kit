package daemonctl

import (
	"encoding/json"
	"net/http"

	"github.com/akonwi/kit/internal/httpapi"
)

const (
	instanceHeader = httpapi.InstanceHeader
	protocolHeader = httpapi.ProtocolHeader
)

// writeJSON answers a fake daemon request the way the daemon encodes responses.
func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
