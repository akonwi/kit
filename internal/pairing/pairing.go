// Package pairing owns pairing codes and links. A pairing code is a
// single-use, 128-bit secret held only in server memory; redeeming it issues a
// device token. A pairing link joins a reachable server URL with a code as
// `https://host/pair#CODE`. The code is in the fragment, so browsers and
// proxies never send or log it.
package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// LinkPath is the path of every pairing link.
	LinkPath = "/pair"
	// CommandLifetime is how long a code issued on request stays valid.
	CommandLifetime = 5 * time.Minute
	// StartupLifetime is how long a code printed at startup stays valid.
	StartupLifetime = time.Hour
	// MaxOutstanding bounds the codes held at once; issuing more forgets the
	// oldest.
	MaxOutstanding = 32

	codeBytes  = 16
	codeLength = 26
)

// ErrInvalidCode reports a code that is unknown, used, or expired.
var ErrInvalidCode = errors.New("pairing code is invalid or expired")

var encoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// ValidCode reports whether code has the pairing code form.
func ValidCode(code string) bool {
	if len(code) != codeLength {
		return false
	}
	decoded, err := encoding.DecodeString(code)
	return err == nil && len(decoded) == codeBytes
}

type outstanding struct {
	hash    [sha256.Size]byte
	expires time.Time
}

// Registry holds outstanding codes in memory.
type Registry struct {
	now   func() time.Time
	mu    sync.Mutex
	codes []outstanding
}

// NewRegistry returns an empty registry; now defaults to time.Now.
func NewRegistry(now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{now: now}
}

// Issue creates a code valid for lifetime and returns it with its expiry.
func (r *Registry) Issue(lifetime time.Duration) (string, time.Time, error) {
	secret := make([]byte, codeBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", time.Time{}, fmt.Errorf("generate pairing code: %w", err)
	}
	code := encoding.EncodeToString(secret)
	now := r.now()
	expires := now.Add(lifetime).UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune(now)
	if len(r.codes) >= MaxOutstanding {
		r.codes = slices.Delete(r.codes, 0, len(r.codes)-MaxOutstanding+1)
	}
	r.codes = append(r.codes, outstanding{hash: sha256.Sum256([]byte(code)), expires: expires})
	return code, expires, nil
}

// Redeem consumes code, failing with ErrInvalidCode when it is unknown, used,
// or expired.
func (r *Registry) Redeem(code string) error {
	if !ValidCode(code) {
		return ErrInvalidCode
	}
	hash := sha256.Sum256([]byte(code))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune(r.now())
	for index, candidate := range r.codes {
		if candidate.hash == hash {
			r.codes = slices.Delete(r.codes, index, index+1)
			return nil
		}
	}
	return ErrInvalidCode
}

func (r *Registry) prune(now time.Time) {
	r.codes = slices.DeleteFunc(r.codes, func(candidate outstanding) bool { return !now.Before(candidate.expires) })
}

// Link joins a server URL and a code.
func Link(serverURL, code string) string {
	return strings.TrimSuffix(serverURL, "/") + LinkPath + "#" + code
}

// ParseLink splits a pairing link into its server URL, an HTTP(S) origin,
// and its code.
func ParseLink(link string) (serverURL, code string, err error) {
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return "", "", fmt.Errorf("parse pairing link: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != LinkPath || parsed.RawQuery != "" || !ValidCode(parsed.Fragment) {
		return "", "", errors.New("pairing link must be an HTTP(S) URL ending in /pair#CODE")
	}
	return parsed.Scheme + "://" + parsed.Host, parsed.Fragment, nil
}

// ValidServedURL reports whether value is an HTTP(S) origin, as ServedURLs
// lists: a scheme and host with an optional port, and nothing else.
func ValidServedURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" &&
		parsed.Scheme+"://"+parsed.Host == value
}

// MaxServedURLs bounds the URLs ServedURLs lists, so every pairing code it
// produces fits the protocol's limit.
const MaxServedURLs = 32

// ServedURLs lists the URLs a server can be reached at, for pairing links:
// each allowed host as an HTTPS URL, or as the allowed origin naming that host
// when one exists, then the extra URLs, then the loopback URL. Duplicates and
// values that are not origins are removed, and the list keeps at most
// MaxServedURLs entries, always ending with the loopback URL.
func ServedURLs(allowedHosts, allowedOrigins []string, loopbackURL string, extra ...string) []string {
	loopback := strings.TrimSuffix(loopbackURL, "/")
	if !ValidServedURL(loopback) {
		loopback = ""
	}
	limit := MaxServedURLs
	if loopback != "" {
		limit--
	}
	var urls []string
	add := func(value string) {
		value = strings.TrimSuffix(value, "/")
		if len(urls) < limit && value != loopback && ValidServedURL(value) && !slices.Contains(urls, value) {
			urls = append(urls, value)
		}
	}
	for _, host := range allowedHosts {
		served := "https://" + bracketIPv6(host)
		for _, origin := range allowedOrigins {
			if parsed, err := url.Parse(origin); err == nil && parsed.Hostname() == host {
				served = origin
				break
			}
		}
		add(served)
	}
	for _, value := range extra {
		add(value)
	}
	if loopback != "" {
		urls = append(urls, loopback)
	}
	return urls
}

func bracketIPv6(host string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}
