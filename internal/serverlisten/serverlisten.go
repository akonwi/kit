// Package serverlisten owns the address the Kit daemon listens on: which
// addresses are accepted, how they are bound, and the loopback URL local
// clients use to reach the bound listener.
package serverlisten

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
)

// Default is the listener used when none is configured: loopback on an
// ephemeral port.
const Default = "127.0.0.1:0"

// Address is a validated listen address. Its host is a loopback IP or an
// unspecified address (0.0.0.0 or ::), which also accepts loopback
// connections, so local clients can always reach the daemon over loopback.
type Address struct {
	host netip.Addr
	port uint16
}

// Parse validates a host:port listen address. Hostnames are rejected so the
// bound interfaces never depend on name resolution, and specific non-loopback
// IPs are rejected because local clients could not reach them over loopback.
func Parse(value string) (Address, error) {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return Address{}, fmt.Errorf("listen address %q must be host:port: %w", value, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return Address{}, fmt.Errorf("listen address %q must use an IP address, not a hostname", value)
	}
	ip = ip.Unmap()
	if !ip.IsLoopback() && !ip.IsUnspecified() {
		return Address{}, fmt.Errorf("listen address %q must use a loopback address or 0.0.0.0/::", value)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return Address{}, fmt.Errorf("listen address %q has an invalid port", value)
	}
	return Address{host: ip, port: uint16(port)}, nil
}

// MustParse parses a known-valid address and panics otherwise.
func MustParse(value string) Address {
	address, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return address
}

// String returns the canonical host:port form.
func (a Address) String() string {
	return netip.AddrPortFrom(a.host, a.port).String()
}

// Network returns the network to listen on for the address family.
func (a Address) Network() string {
	if a.host.Is4() {
		return "tcp4"
	}
	return "tcp6"
}

// Exposed reports whether the address accepts connections beyond loopback.
func (a Address) Exposed() bool {
	return a.host.IsUnspecified()
}

// Listen binds the address.
func (a Address) Listen() (net.Listener, error) {
	return net.Listen(a.Network(), a.String())
}

// LoopbackURL returns the URL local clients use to reach a listener bound to
// this address. For an unspecified host it is the loopback address of the same
// family, on the bound port.
func (a Address) LoopbackURL(bound net.Addr) (string, error) {
	tcp, ok := bound.(*net.TCPAddr)
	if !ok || tcp.Port <= 0 || tcp.Port > 65535 {
		return "", errors.New("listener is not bound to a TCP port")
	}
	host := a.host
	if host.IsUnspecified() {
		if host.Is4() {
			host = netip.AddrFrom4([4]byte{127, 0, 0, 1})
		} else {
			host = netip.IPv6Loopback()
		}
	}
	return "http://" + netip.AddrPortFrom(host, uint16(tcp.Port)).String(), nil
}

// Serves reports whether a daemon published at publishedURL could have been
// started with this address: the published loopback host must belong to the
// address and, unless the address is ephemeral, the port must match.
func (a Address) Serves(publishedURL string) bool {
	parsed, err := url.Parse(publishedURL)
	if err != nil {
		return false
	}
	published, err := netip.ParseAddrPort(parsed.Host)
	if err != nil {
		return false
	}
	if a.port != 0 && published.Port() != a.port {
		return false
	}
	host := published.Addr().Unmap()
	if a.host.IsUnspecified() {
		return host.IsLoopback() && host.Is4() == a.host.Is4()
	}
	return host == a.host
}
