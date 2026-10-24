// Package openurl provides the open_url tool, which asks the user's attached
// Kit app to open a web address. The server only validates the address and
// records it in the result details; an attached client that supports the tool
// opens it on its own machine when it observes the completed call live. No
// client acknowledges the request, so the result does not report whether
// anything opened.
package openurl

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"

	"github.com/akonwi/kit/droids"
)

const (
	// ToolName is the model-facing tool name clients recognize.
	ToolName = "open_url"
	// Presentation marks result details as a client open request.
	Presentation = "open-url"
	// MaxURLBytes bounds the requested address.
	MaxURLBytes = 2 << 10
)

type arguments struct {
	URL string `json:"url"`
}

// Details is the typed presentation marker persisted with a successful
// result. URL is the validated, canonical address a client may open.
type Details struct {
	Presentation string `json:"presentation"`
	URL          string `json:"url"`
}

// New constructs the open_url tool.
func New() droids.AnyTool {
	return droids.MustTool(droids.Tool[arguments]{
		Name: ToolName,
		Description: "Ask the user's Kit app to open an http or https URL in a browser tab. " +
			"The URL is opened on the user's machine, so localhost refers to that machine. " +
			"Returns true once the request is recorded; it does not confirm that a client opened it.",
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "minLength": 1, "maxLength": MaxURLBytes,
					"description": "Absolute http or https URL to open."},
			},
			"required": []any{"url"},
		},
		Mode: droids.ModeParallel,
		Execute: func(_ context.Context, _ droids.ToolContext, args arguments, _ droids.ToolUpdate) (droids.ToolResult, error) {
			return execute(args)
		},
	})
}

func execute(args arguments) (droids.ToolResult, error) {
	canonical, err := Canonical(args.URL)
	if err != nil {
		return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: "Error: " + err.Error()}}, IsError: true}, nil
	}
	encoded, err := droids.EncodeDetails(Details{Presentation: Presentation, URL: canonical})
	if err != nil {
		return droids.ToolResult{}, err
	}
	return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: "true"}}, Details: encoded}, nil
}

// Canonical validates raw as an absolute http or https URL with a host and no
// credentials, and returns its canonical form.
func Canonical(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("url is required")
	}
	if len(raw) > MaxURLBytes {
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
	if len(canonical) > MaxURLBytes {
		return "", errors.New("url is too long")
	}
	return canonical, nil
}

// ParseDetails accepts only the complete open_url presentation shape whose
// address is already canonical.
func ParseDetails(raw json.RawMessage) (Details, bool) {
	var details Details
	if len(raw) == 0 || json.Unmarshal(raw, &details) != nil || details.Presentation != Presentation {
		return Details{}, false
	}
	canonical, err := Canonical(details.URL)
	if err != nil || canonical != details.URL {
		return Details{}, false
	}
	return details, true
}
