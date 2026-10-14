// Package serveraccess owns which Host and Origin headers the Kit daemon
// accepts. It keeps DNS-rebinding protection while permitting loopback
// connections on any port, which is how SSH-forwarded and local clients
// arrive, and explicitly configured hostnames and origins, which is how
// proxied and tunneled clients arrive.
package serveraccess

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// Policy is the set of hosts and origins the daemon accepts beyond loopback.
// The zero Policy accepts only loopback hosts and same-host origins.
type Policy struct {
	hosts   map[string]bool
	origins map[string]bool
}

// NewPolicy builds a policy from allowed hosts and origins, which must already
// be canonical values returned by ParseHost and ParseOrigin.
func NewPolicy(hosts, origins []string) Policy {
	policy := Policy{hosts: make(map[string]bool, len(hosts)), origins: make(map[string]bool, len(origins))}
	for _, host := range hosts {
		policy.hosts[host] = true
	}
	for _, origin := range origins {
		policy.origins[origin] = true
	}
	return policy
}

// ParseHost validates an allowed host and returns its canonical form: a
// lowercase DNS name or an IP address, without a port. An allowed host matches
// requests on any port.
func ParseHost(value string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(value))
	if address, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil && address.Zone() == "" {
		return address.Unmap().String(), nil
	}
	host = strings.TrimSuffix(host, ".")
	if !validDNSName(host) {
		return "", fmt.Errorf("allowed host %q must be a DNS name or IP address without a port", value)
	}
	return host, nil
}

// ParseOrigin validates an allowed origin and returns its canonical
// scheme://host[:port] form.
func ParseOrigin(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", fmt.Errorf("allowed origin %q must be an http or https origin without a path", value)
	}
	host, port := splitHost(parsed.Host)
	canonical, err := ParseHost(host)
	if err != nil {
		return "", fmt.Errorf("allowed origin %q: %w", value, err)
	}
	if strings.Contains(canonical, ":") {
		canonical = "[" + canonical + "]"
	}
	if port != "" {
		canonical += ":" + port
	}
	return parsed.Scheme + "://" + canonical, nil
}

// HostAllowed reports whether a request Host header names this daemon: a
// loopback IP address on any port, or an allowed host on any port.
func (p Policy) HostAllowed(requestHost string) bool {
	host, _ := splitHost(requestHost)
	if host == "" {
		return false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		address = address.Unmap()
		return address.Zone() == "" && (address.IsLoopback() || p.hosts[address.String()])
	}
	return p.hosts[strings.TrimSuffix(strings.ToLower(host), ".")]
}

// OriginAllowed reports whether a request's Origin header is acceptable. An
// absent Origin is allowed, since only browsers send it. Otherwise the origin
// must name the request's own host, under either scheme because TLS may be
// terminated by a proxy, or be an allowed origin.
func (p Policy) OriginAllowed(origin, requestHost string) bool {
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	if strings.EqualFold(parsed.Host, requestHost) {
		return true
	}
	canonical, err := ParseOrigin(origin)
	return err == nil && p.origins[canonical]
}

// splitHost separates a Host header into host and port, removing IPv6
// brackets.
func splitHost(value string) (host, port string) {
	if host, port, err := net.SplitHostPort(value); err == nil {
		return host, port
	}
	return strings.Trim(value, "[]"), ""
}

func validDNSName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
