// Package serverstatus defines the machine-readable report printed by
// `kit server status --json`. The SSH connector reads it from another host,
// whose Kit may be a different version, before checking protocol
// compatibility, so the report's fields are stable: they may be added to but
// never renamed, retyped, or removed.
package serverstatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// MaxBytes bounds a report a reader accepts.
const MaxBytes = 16 << 10

// Report describes a running Kit server for a program that connects to it.
// It includes the server's local token, a secret equivalent to access to the
// server's Kit home.
type Report struct {
	// Home is the absolute path of the server's Kit home.
	Home string `json:"home"`
	// PID is the server process.
	PID int `json:"pid"`
	// URL is the server's HTTP origin on its host.
	URL string `json:"url"`
	// KitVersion and ProtocolVersion identify the server's build and its
	// session protocol.
	KitVersion      string `json:"kitVersion"`
	ProtocolVersion int    `json:"protocolVersion"`
	// InstanceID identifies this run of the server.
	InstanceID string `json:"instanceId"`
	// DatabaseReady reports whether the server can serve sessions.
	DatabaseReady bool `json:"databaseReady"`
	// Token is the server's local access token.
	Token string `json:"token"`
}

// Decode reads one report and validates it.
func Decode(data []byte) (Report, error) {
	if len(data) > MaxBytes {
		return Report{}, fmt.Errorf("server status report exceeds %d bytes", MaxBytes)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return Report{}, fmt.Errorf("decode server status report: %w", err)
	}
	return report, report.Validate()
}

// Validate checks a report's fields.
func (r Report) Validate() error {
	switch {
	case r.PID <= 0:
		return errors.New("server status report has no pid")
	case r.ProtocolVersion <= 0:
		return errors.New("server status report has no protocol version")
	case !printable(r.KitVersion, 128) || r.KitVersion == "":
		return errors.New("server status report has an invalid Kit version")
	case !printable(r.InstanceID, 128) || r.InstanceID == "":
		return errors.New("server status report has an invalid instance id")
	case !printable(r.Token, 512) || r.Token == "":
		return errors.New("server status report has an invalid token")
	case !printable(r.Home, 4096):
		return errors.New("server status report has an invalid home")
	}
	if _, _, err := r.Address(); err != nil {
		return err
	}
	return nil
}

// Address returns the host and port of the report's URL, which must be a
// plain HTTP origin on a loopback address, as a server publishes it.
func (r Report) Address() (string, int, error) {
	parsed, err := url.Parse(r.URL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", 0, fmt.Errorf("server status report has an invalid url %q", r.URL)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("server status report url %q has no port", r.URL)
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return "", 0, fmt.Errorf("server status report url %q is not on a loopback address", r.URL)
	}
	return host, port, nil
}

func printable(value string, limit int) bool {
	if len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
