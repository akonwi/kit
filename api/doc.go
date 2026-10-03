// Package kit provides the typed, stateful Go client for a Kit server.
//
// Connect authenticates and checks protocol compatibility before returning a
// concurrent Client. Local targets only discover the daemon registered under
// the effective Kit home; Connect(Local()) never starts, replaces, stops, or
// migrates a daemon. Applications that own local daemon lifecycle must do that
// before connecting.
//
// Sessions are immutable attachments owned by a Client. Client.Close cancels
// client-owned requests and streams, but canceling a caller context only
// detaches that caller. It does not abort a server-side turn or bash execution;
// use the corresponding handle's Abort method when abort is intended.
//
// Watch streams reconnect internally. A Session watch begins with an
// authoritative snapshot and may emit later replacement snapshots after replay
// resynchronization. Callers should replace their projected state whenever an
// update contains a snapshot.
//
// Public records are aliases of package contract records and retain their
// canonical wire values. Additive methods, fields, and declared error codes are
// compatible SDK evolution. Connect rejects incompatible server protocol
// versions with ErrIncompatibleServer. Operation failures are exposed through
// ServerError, transport failures through TransportError, and malformed server
// responses through ProtocolError; callers should use errors.Is/errors.As
// rather than matching error strings.
package kit

//go:generate go run ./internal/cmd/genaliases -contract ./contract -output ./contract_aliases.generated.go
