// Package weburl validates the web addresses Kit asks a client to open.
//
// Both the open_url model tool and the plugin kit/system/open-url request use
// these rules, and the session contract applies them to the addresses it
// carries, so every path accepts exactly the same addresses. The package has
// no dependencies so that the public contract can use it.
package weburl

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// MaxBytes bounds a requested address, before and after canonicalization.
const MaxBytes = 2 << 10

// Canonical validates raw as an absolute http or https URL with a host and no
// credentials, and returns its canonical form. Error messages are concise and
// safe to show to the requester.
func Canonical(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("url is required")
	}
	if len(raw) > MaxBytes {
		return "", errors.New("url is too long")
	}
	if strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", errors.New("url must not contain whitespace or control characters")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("url is not valid")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("url must use http or https")
	}
	if parsed.Opaque != "" || parsed.Hostname() == "" {
		return "", errors.New("url must include a host")
	}
	if parsed.User != nil {
		return "", errors.New("url must not include credentials")
	}
	canonical := parsed.String()
	if len(canonical) > MaxBytes {
		return "", errors.New("url is too long")
	}
	return canonical, nil
}

// IsCanonical reports whether value is a valid address already in canonical
// form, as every address carried across a process boundary must be.
func IsCanonical(value string) bool {
	canonical, err := Canonical(value)
	return err == nil && canonical == value
}
