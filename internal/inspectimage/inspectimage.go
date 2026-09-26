// Package inspectimage provides the explicit, attachment-backed inspect_image tool.
package inspectimage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/localimage"
	"github.com/akonwi/kit/internal/modelimage"
)

const ToolName = "inspect_image"

// CWDProvider returns the current cwd of one session.
type CWDProvider func() string

// Options supplies the session-owned resources used by inspect_image.
type Options struct {
	SessionID string
	CWD       CWDProvider
	Store     attachment.Store
}

type arguments struct {
	Path string `json:"path"`
}

// Details is the typed attachment metadata persisted with a successful result.
type Details struct {
	AttachmentID string `json:"attachmentId"`
	Filename     string `json:"filename"`
	MediaType    string `json:"mediaType"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
}

// New constructs a session-bound inspect_image tool.
func New(options Options) (droids.AnyTool, error) {
	if strings.TrimSpace(options.SessionID) == "" || options.CWD == nil || options.Store == nil {
		return nil, errors.New("inspectimage: session ID, cwd provider, and store are required")
	}
	return droids.NewTool(droids.Tool[arguments]{
		Name:        ToolName,
		Description: "Inspect a local PNG, JPEG, GIF, or WebP image when its visual content is needed for your reasoning.",
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "minLength": 1},
			},
			"required": []any{"path"},
		},
		Execute: func(ctx context.Context, _ droids.ToolContext, args arguments, _ droids.ToolUpdate) (droids.ToolResult, error) {
			return execute(ctx, options, args)
		},
	})
}

func execute(ctx context.Context, options Options, args arguments) (droids.ToolResult, error) {
	record, err := localimage.Persist(ctx, localimage.Options{
		SessionID: options.SessionID, CWD: localimage.CWDProvider(options.CWD), Store: options.Store,
		Limits: modelimage.Limits(),
	}, args.Path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return droids.ToolResult{}, ctxErr
		}
		return failure(err.Error()), nil
	}
	stored, reader, err := options.Store.Open(ctx, options.SessionID, record.ID)
	if err != nil {
		remove(ctx, options, record.ID)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return droids.ToolResult{}, ctxErr
		}
		return droids.ToolResult{}, fmt.Errorf("open persisted image: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, stored.Size+1))
	closeErr := reader.Close()
	if ctxErr := ctx.Err(); ctxErr != nil {
		remove(ctx, options, record.ID)
		return droids.ToolResult{}, ctxErr
	}
	if readErr != nil || closeErr != nil || int64(len(data)) != stored.Size {
		remove(ctx, options, record.ID)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return droids.ToolResult{}, ctxErr
		}
		switch {
		case readErr != nil:
			return droids.ToolResult{}, fmt.Errorf("read persisted image: %w", readErr)
		case closeErr != nil:
			return droids.ToolResult{}, fmt.Errorf("close persisted image: %w", closeErr)
		default:
			return droids.ToolResult{}, errors.New("read persisted image: size changed")
		}
	}
	details, err := droids.EncodeDetails(Details{
		AttachmentID: stored.ID, Filename: stored.Filename, MediaType: stored.MediaType,
		Width: stored.Width, Height: stored.Height,
	})
	if err != nil {
		remove(ctx, options, record.ID)
		return droids.ToolResult{}, err
	}
	file := droids.NewFileData(stored.Filename, stored.MediaType, data)
	file.AttachmentID = stored.ID
	return droids.ToolResult{
		Content: []droids.ResultContent{
			droids.TextContent{Text: "Image loaded for inspection."},
			file,
		},
		Details: details,
	}, nil
}

func remove(ctx context.Context, options Options, attachmentID string) {
	_ = options.Store.Remove(context.WithoutCancel(ctx), options.SessionID, attachmentID)
}

func failure(message string) droids.ToolResult {
	return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: "Error: " + message}}, IsError: true}
}
