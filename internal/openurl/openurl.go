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

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/weburl"
)

const (
	// ToolName is the model-facing tool name clients recognize.
	ToolName = "open_url"
	// Presentation marks result details as a client open request.
	Presentation = "open-url"
	// MaxURLBytes bounds the requested address.
	MaxURLBytes = weburl.MaxBytes
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
// credentials, and returns its canonical form. It applies the same rules as
// plugin URL requests (see package weburl).
func Canonical(raw string) (string, error) { return weburl.Canonical(raw) }

// ParseDetails accepts only the complete open_url presentation shape whose
// address is already canonical.
func ParseDetails(raw json.RawMessage) (Details, bool) {
	var details Details
	if len(raw) == 0 || json.Unmarshal(raw, &details) != nil || details.Presentation != Presentation {
		return Details{}, false
	}
	if !weburl.IsCanonical(details.URL) {
		return Details{}, false
	}
	return details, true
}
