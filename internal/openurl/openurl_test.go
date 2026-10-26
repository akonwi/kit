package openurl

import (
	"encoding/json"
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
