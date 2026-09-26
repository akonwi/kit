// Package localimage validates and persists stable local image files.
package localimage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/akonwi/kit/internal/attachment"
)

// CWDProvider returns one session's current working directory.
type CWDProvider func() string

// Options supplies the session-owned resources and limits for one local image.
type Options struct {
	SessionID string
	CWD       CWDProvider
	Store     attachment.Store
	Limits    attachment.ImageLimits
}

// Persist resolves, validates, and stores a stable local image file.
func Persist(ctx context.Context, options Options, requestedPath string) (attachment.Record, error) {
	if strings.TrimSpace(options.SessionID) == "" || options.CWD == nil || options.Store == nil {
		return attachment.Record{}, errors.New("localimage: session ID, cwd provider, and store are required")
	}
	if strings.TrimSpace(requestedPath) == "" {
		return attachment.Record{}, errors.New("path is required")
	}
	path := requestedPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(options.CWD(), path)
	}
	path = filepath.Clean(path)
	file, err := os.Open(path)
	if err != nil {
		return attachment.Record{}, err
	}
	initial, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return attachment.Record{}, err
	}
	if !initial.Mode().IsRegular() {
		_ = file.Close()
		return attachment.Record{}, errors.New("path is not a regular file")
	}
	input, err := attachment.InspectImage(filepath.Base(path), file, options.Limits)
	if err != nil {
		_ = file.Close()
		return attachment.Record{}, err
	}
	input.SessionID = options.SessionID
	record, err := options.Store.Put(ctx, input)
	final, statErr := file.Stat()
	current, pathStatErr := os.Stat(path)
	closeErr := file.Close()
	if err != nil {
		return attachment.Record{}, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = options.Store.Remove(context.WithoutCancel(ctx), options.SessionID, record.ID)
		return attachment.Record{}, ctxErr
	}
	if statErr != nil || pathStatErr != nil || closeErr != nil || !os.SameFile(initial, current) ||
		final.Size() != initial.Size() || !final.ModTime().Equal(initial.ModTime()) {
		_ = options.Store.Remove(context.WithoutCancel(ctx), options.SessionID, record.ID)
		return attachment.Record{}, errors.New("image changed while it was being read")
	}
	return record, nil
}
