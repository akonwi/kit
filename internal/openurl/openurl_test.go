package openurl

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/akonwi/kit/droids"
)

func TestExecuteReturnsTrueWithCanonicalDetails(t *testing.T) {
	t.Parallel()
	result, err := execute(arguments{URL: "HTTPS://example.com/docs?q=1#top"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("result = %#v, want one successful content part", result)
	}
	if text, ok := result.Content[0].(droids.TextContent); !ok || text.Text != "true" {
		t.Fatalf("content = %#v, want text true", result.Content)
	}
	if string(result.Details) != `{"presentation":"open-url","url":"https://example.com/docs?q=1#top"}` {
		t.Fatalf("details = %s", result.Details)
	}
	details, ok := ParseDetails(result.Details)
	if !ok || details != (Details{Presentation: Presentation, URL: "https://example.com/docs?q=1#top"}) {
		t.Fatalf("ParseDetails() = %#v, %v", details, ok)
	}
}

func TestExecuteRejectsInvalidURLAsToolError(t *testing.T) {
	t.Parallel()
	result, err := execute(arguments{URL: "file:///etc/passwd"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.Details != nil || len(result.Content) != 1 {
		t.Fatalf("result = %#v, want error without details", result)
	}
	if text, ok := result.Content[0].(droids.TextContent); !ok || text.Text != "Error: url must use http or https" {
		t.Fatalf("content = %#v", result.Content)
	}
}

func TestCanonical(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		raw, want, err string
	}{
		{raw: "http://localhost:3000/", want: "http://localhost:3000/"},
		{raw: "https://[::1]:8443/a", want: "https://[::1]:8443/a"},
		{raw: "HTTP://Example.com", want: "http://Example.com"},
		{raw: "", err: "url is required"},
		{raw: "https://example.com/" + strings.Repeat("a", MaxURLBytes), err: "url is too long"},
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

func TestParseDetailsRejectsUnexpectedShapes(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		``,
		`{}`,
		`{"presentation":"transcript-image","url":"https://example.com"}`,
		`{"presentation":"open-url","url":"file:///tmp/a.html"}`,
		`{"presentation":"open-url","url":"HTTPS://example.com"}`,
	} {
		if details, ok := ParseDetails(json.RawMessage(raw)); ok {
			t.Errorf("ParseDetails(%s) = %#v, want rejection", raw, details)
		}
	}
}
