// Package showimage provides the explicit, attachment-backed show_image tool.
package showimage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/attachmentmeta"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/localimage"
)

const (
	ToolName        = "show_image"
	Presentation    = "transcript-image"
	maxImageBytes   = 16 << 20
	maxImagePixels  = 24_000_000
	maxCaptionRunes = 200
)

// CWDProvider returns the current cwd of one session.
type CWDProvider func() string

// Options supplies the session-owned resources used by show_image.
type Options struct {
	SessionID string
	CWD       CWDProvider
	Store     attachment.Store
}

type arguments struct {
	Path    string `json:"path"`
	Caption string `json:"caption,omitempty"`
}

// Details is the typed presentation marker persisted with a successful result.
type Details struct {
	Presentation string `json:"presentation"`
	AttachmentID string `json:"attachmentId"`
	Filename     string `json:"filename"`
	MediaType    string `json:"mediaType"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Caption      string `json:"caption,omitempty"`
}

// New constructs a session-bound show_image tool.
func New(options Options) (droids.AnyTool, error) {
	if strings.TrimSpace(options.SessionID) == "" || options.CWD == nil || options.Store == nil {
		return nil, errors.New("showimage: session ID, cwd provider, and store are required")
	}
	return droids.NewTool(droids.Tool[arguments]{
		Name:        ToolName,
		Description: "Present a local PNG, JPEG, GIF, or WebP image to the user.",
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "minLength": 1},
				"caption": map[string]any{"type": "string", "maxLength": maxCaptionRunes},
			},
			"required": []any{"path"},
		},
		Execute: func(ctx context.Context, _ droids.ToolContext, args arguments, _ droids.ToolUpdate) (droids.ToolResult, error) {
			return execute(ctx, options, args)
		},
	})
}

func execute(ctx context.Context, options Options, args arguments) (droids.ToolResult, error) {
	if strings.TrimSpace(args.Path) == "" {
		return failure("path is required"), nil
	}
	if utf8.RuneCountInString(args.Caption) > maxCaptionRunes {
		return failure(fmt.Sprintf("caption exceeds %d characters", maxCaptionRunes)), nil
	}
	record, err := localimage.Persist(ctx, localimage.Options{
		SessionID: options.SessionID, CWD: localimage.CWDProvider(options.CWD), Store: options.Store,
		Limits: attachment.ImageLimits{MaxBytes: maxImageBytes, MaxPixels: maxImagePixels},
	}, args.Path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return droids.ToolResult{}, ctxErr
		}
		return failure(err.Error()), nil
	}
	details := Details{
		Presentation: Presentation, AttachmentID: record.ID, Filename: record.Filename,
		MediaType: record.MediaType, Width: record.Width, Height: record.Height, Caption: args.Caption,
	}
	encoded, err := droids.EncodeDetails(details)
	if err != nil {
		return droids.ToolResult{}, err
	}
	return droids.ToolResult{
		Content: []droids.ResultContent{droids.TextContent{Text: "Image displayed successfully."}},
		Details: encoded,
	}, nil
}

// ParseDetails accepts only the explicit, complete show_image presentation
// shape used to promote a tool image into transcript content.
func ParseDetails(raw json.RawMessage) (Details, bool) {
	var details Details
	if len(raw) == 0 || json.Unmarshal(raw, &details) != nil || details.Presentation != Presentation ||
		!identifier.Valid(details.AttachmentID, "attachment_") || !attachmentmeta.ValidFilename(details.Filename) ||
		!strings.HasPrefix(details.MediaType, "image/") || !attachmentmeta.SupportedMediaType(details.MediaType) ||
		details.Width <= 0 || details.Height <= 0 || int64(details.Width)*int64(details.Height) > maxImagePixels ||
		utf8.RuneCountInString(details.Caption) > maxCaptionRunes {
		return Details{}, false
	}
	return details, true
}

func failure(message string) droids.ToolResult {
	return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: "Error: " + message}}, IsError: true}
}
