package weburl

import (
	"strings"
	"testing"
)

func TestCanonical(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		raw, want, err string
	}{
		{raw: "http://localhost:3000/", want: "http://localhost:3000/"},
		{raw: "https://[::1]:8443/a", want: "https://[::1]:8443/a"},
		{raw: "HTTP://Example.com", want: "http://Example.com"},
		{raw: "", err: "url is required"},
		{raw: "https://example.com/" + strings.Repeat("a", MaxBytes), err: "url is too long"},
		{raw: "https://example.com/a b", err: "url must not contain whitespace or control characters"},
		{raw: "https://example.com/\x00", err: "url must not contain whitespace or control characters"},
		{raw: "javascript:alert(1)", err: "url must use http or https"},
		{raw: "example.com", err: "url must use http or https"},
		{raw: "https:example.com", err: "url must include a host"},
		{raw: "https:///path", err: "url must include a host"},
		{raw: "https://user:secret@example.com", err: "url must not include credentials"},
		{raw: "https://%zz", err: "url is not valid"},
	} {
		got, err := Canonical(test.raw)
		if test.err != "" {
			if err == nil || err.Error() != test.err {
				t.Errorf("Canonical(%q) error = %v, want %q", test.raw, err, test.err)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("Canonical(%q) = %q, %v; want %q", test.raw, got, err, test.want)
		}
	}
}

func TestIsCanonicalAcceptsOnlyCanonicalWebAddresses(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{
		"https://example.com/docs?q=1#top": true,
		"http://localhost:3000/":           true,
		"HTTPS://example.com/":             false,
		"file:///tmp/page.html":            false,
		"https://user@example.com/":        false,
		"":                                 false,
	} {
		if got := IsCanonical(value); got != want {
			t.Errorf("IsCanonical(%q) = %t, want %t", value, got, want)
		}
	}
}
