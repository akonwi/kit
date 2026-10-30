// Package saveattachment provides the save_attachment tool, which copies one
// of a session's attachments to a file on the server so the model can reuse
// an image a tool returned.
package saveattachment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/identifier"
)

// ToolName is the model-facing tool name.
const ToolName = "save_attachment"

// savedMode is the permission of a saved file: an ordinary user file.
const savedMode = 0o644

// CWDProvider returns the current cwd of one session.
type CWDProvider func() string

// Options supplies the session-owned resources used by save_attachment.
type Options struct {
	// SessionID owns the attachments the tool may read.
	SessionID string
	// CWD resolves relative destinations.
	CWD   CWDProvider
	Store attachment.Store
	// Protected lists directories the tool refuses to write within, such as
	// the Kit home that holds attachment storage and credentials.
	Protected []string
}

type arguments struct {
	Attachment string `json:"attachment"`
	Path       string `json:"path"`
	Overwrite  bool   `json:"overwrite,omitempty"`
}

// Details is the metadata persisted with a successful result.
type Details struct {
	AttachmentID string `json:"attachmentId"`
	Path         string `json:"path"`
	MediaType    string `json:"mediaType"`
	Size         int64  `json:"size"`
}

// Reference is the model-visible line that names a stored attachment so the
// model can pass its ID to save_attachment.
func Reference(record attachment.Record) string {
	var reference strings.Builder
	fmt.Fprintf(&reference, "Attachment %s: %s", record.ID, record.MediaType)
	if record.Width > 0 && record.Height > 0 {
		fmt.Fprintf(&reference, ", %d×%d", record.Width, record.Height)
	}
	fmt.Fprintf(&reference, ", %s. %s copies it to a file.", formatSize(record.Size), ToolName)
	return reference.String()
}

// Unavailable is the model-visible line for an image that was not stored, so
// the model does not look for an attachment that does not exist.
const Unavailable = "This image was not kept as an attachment, so " + ToolName + " cannot copy it."

// New constructs a session-bound save_attachment tool.
func New(options Options) (droids.AnyTool, error) {
	if strings.TrimSpace(options.SessionID) == "" || options.CWD == nil || options.Store == nil {
		return nil, errors.New("saveattachment: session ID, cwd provider, and store are required")
	}
	for _, root := range options.Protected {
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("saveattachment: protected directory %q is not absolute", root)
		}
	}
	return droids.NewTool(droids.Tool[arguments]{
		Name: ToolName,
		Description: "Copy a session attachment, such as an image a tool returned, to a file on the server, " +
			"so it can be committed, attached, processed, or inspected again. Tool results name attachments as \"Attachment attachment_…\".",
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"attachment": map[string]any{"type": "string", "minLength": 1, "description": "Attachment ID, like attachment_0123…"},
				"path":       map[string]any{"type": "string", "minLength": 1, "description": "Destination file, relative to cwd or absolute. Missing parent directories are created."},
				"overwrite":  map[string]any{"type": "boolean", "description": "Replace an existing file. Default false."},
			},
			"required": []any{"attachment", "path"},
		},
		Mode: droids.ModeSequential,
		Execute: func(ctx context.Context, _ droids.ToolContext, args arguments, _ droids.ToolUpdate) (droids.ToolResult, error) {
			return execute(ctx, options, args)
		},
	})
}

func execute(ctx context.Context, options Options, args arguments) (droids.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return droids.ToolResult{}, err
	}
	id := strings.TrimSpace(args.Attachment)
	if !identifier.Valid(id, attachment.IDPrefix) {
		return failure(fmt.Sprintf("%q is not an attachment ID", args.Attachment)), nil
	}
	path, err := destination(options, args.Path)
	if err != nil {
		return failure(err.Error()), nil
	}
	record, reader, err := options.Store.Open(ctx, options.SessionID, id)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return droids.ToolResult{}, ctxErr
		}
		if errors.Is(err, attachment.ErrNotFound) {
			return failure(fmt.Sprintf("attachment %s is not available in this session", id)), nil
		}
		return droids.ToolResult{}, fmt.Errorf("open attachment: %w", err)
	}
	defer reader.Close()
	if err := write(ctx, options, path, args.Overwrite, record, reader); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return droids.ToolResult{}, ctxErr
		}
		return failure(err.Error()), nil
	}
	details, err := droids.EncodeDetails(Details{AttachmentID: record.ID, Path: path, MediaType: record.MediaType, Size: record.Size})
	if err != nil {
		return droids.ToolResult{}, err
	}
	return droids.ToolResult{
		Content: []droids.ResultContent{droids.TextContent{Text: fmt.Sprintf("Saved %s (%s, %s) to %s", record.ID, record.MediaType, formatSize(record.Size), path)}},
		Details: details,
	}, nil
}

// destination resolves the requested path against the session cwd and refuses
// protected directories, following symbolic links in its existing ancestors.
func destination(options Options, requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return "", errors.New("path is required")
	}
	if strings.IndexByte(requested, 0) >= 0 {
		return "", errors.New("path contains NUL")
	}
	path := requested
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(options.CWD(), path)
	}
	path = filepath.Clean(path)
	if path == string(filepath.Separator) {
		return "", errors.New("path names a directory")
	}
	if err := checkProtected(options.Protected, path); err != nil {
		return "", err
	}
	return path, nil
}

func checkProtected(protected []string, path string) error {
	resolved, err := resolveExisting(path)
	if err != nil {
		return err
	}
	for _, root := range protected {
		for _, candidate := range []string{filepath.Clean(root), resolvedRoot(root)} {
			if within(candidate, path) || within(candidate, resolved) {
				return fmt.Errorf("refusing to write inside %s, which Kit manages", filepath.Clean(root))
			}
		}
	}
	return nil
}

// resolveExisting follows symbolic links through the deepest existing
// ancestor of path and appends the components that do not exist yet.
func resolveExisting(path string) (string, error) {
	existing, rest := path, ""
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve path: %w", err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return path, nil
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
}

func resolvedRoot(root string) string {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	return resolved
}

func within(root, path string) bool {
	if root == "" {
		return false
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// write stages the attachment beside path and publishes it with a rename, so
// a reader never sees a partial file. Without overwrite, publication fails if
// anything exists at path; with it, only a regular file is replaced.
func write(ctx context.Context, options Options, path string, overwrite bool, record attachment.Record, content io.Reader) (resultErr error) {
	if info, err := os.Lstat(path); err == nil {
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			return errors.New("path is a symbolic link")
		case !info.Mode().IsRegular():
			return errors.New("path exists and is not a regular file")
		case !overwrite:
			return errors.New("path already exists; pass overwrite to replace it")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect path: %w", err)
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}
	// Re-check after creating directories: a link created meanwhile must not
	// redirect the write into a protected directory.
	if err := checkProtected(options.Protected, path); err != nil {
		return err
	}
	staged, err := os.CreateTemp(parent, ".kit-save-*")
	if err != nil {
		return fmt.Errorf("stage file: %w", err)
	}
	stagedPath := staged.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(stagedPath)
		}
	}()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(staged, hash), io.LimitReader(contextReader{ctx: ctx, reader: content}, record.Size+1))
	if copyErr == nil && (written != record.Size || hex.EncodeToString(hash.Sum(nil)) != record.SHA256) {
		copyErr = errors.New("attachment content does not match its record")
	}
	if copyErr == nil {
		copyErr = staged.Chmod(savedMode)
	}
	if copyErr == nil {
		copyErr = staged.Sync()
	}
	if closeErr := staged.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return fmt.Errorf("write file: %w", copyErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if overwrite {
		if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
			return errors.New("path exists and is not a regular file")
		}
		if err := os.Rename(stagedPath, path); err != nil {
			return fmt.Errorf("publish file: %w", err)
		}
		return nil
	}
	// A hard link publishes without replacing anything created meanwhile.
	// Where the file system has no hard links, a rename after a fresh check
	// is the closest equivalent.
	if err := os.Link(stagedPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("path already exists; pass overwrite to replace it")
		}
		if _, statErr := os.Lstat(path); statErr == nil {
			return errors.New("path already exists; pass overwrite to replace it")
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("publish file: %w", err)
		}
		if err := os.Rename(stagedPath, path); err != nil {
			return fmt.Errorf("publish file: %w", err)
		}
		return nil
	}
	_ = os.Remove(stagedPath)
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func formatSize(size int64) string {
	switch {
	case size < 1<<10:
		return fmt.Sprintf("%d bytes", size)
	case size < 1<<20:
		return fmt.Sprintf("%.0f KiB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
	}
}

func failure(message string) droids.ToolResult {
	return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: "Error: " + message}}, IsError: true}
}
