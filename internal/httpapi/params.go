package httpapi

const (
	// InstanceHeader binds requests to one daemon process identity.
	InstanceHeader = "X-Kit-Instance-ID"
	// ProtocolHeader declares the client's session protocol version.
	ProtocolHeader = "X-Kit-Protocol-Version"
)

// SessionPath binds the sessionID path parameter.
type SessionPath struct {
	SessionID string `path:"sessionID"`
}
