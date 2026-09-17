package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/akonwi/kit/internal/protocol"
)

// DiffEntry is descriptor-anchored evidence for one literal workspace path.
type DiffEntry struct {
	Path, Kind, Identity string
	Mode                 uint32
	Size                 int64
	Data                 []byte
	Digest               [32]byte
	Unavailable          bool
	Limit                string
}

// DiffSnapshot is a coherent set of individually revalidated path observations.
type DiffSnapshot struct {
	Workspace    protocol.WorkspaceRef
	Entries      []DiffEntry
	RootIdentity string
}

// ObserveDiffPaths is the narrow secure workspace port used by the diff service.
// It never follows the final symlink and never authorizes repository metadata.
func (s *Service) ObserveDiffPaths(ctx context.Context, sessionID, cwd, expected string, paths []string, maxFileBytes int, aggregate *int64) (DiffSnapshot, error) {
	ref, err := s.check(sessionID, cwd, expected)
	if err != nil {
		return DiffSnapshot{}, err
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return DiffSnapshot{}, classify(err, NotDirectory)
	}
	defer root.Close()
	ri, err := root.Lstat(".")
	if err != nil {
		return DiffSnapshot{}, classify(err, NotDirectory)
	}
	dev, ino, _ := hostFileIdentity(ri)
	rootSum := sha256.Sum256([]byte(fmt.Sprintf("diff-root-v1:%d:%d", dev, ino)))
	out := DiffSnapshot{Workspace: ref, RootIdentity: base64.RawURLEncoding.EncodeToString(rootSum[:]), Entries: make([]DiffEntry, 0, len(paths))}
	checks := 0
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return DiffSnapshot{}, err
		}
		if protocol.ValidateWorkspacePath(p, false) != nil {
			return DiffSnapshot{}, &Error{Code: InvalidPath, Message: "workspace path is invalid"}
		}
		parent, name, closeParent, e := openParentPath(root, p, &checks)
		if e != nil {
			var typed *Error
			if errors.As(e, &typed) && typed.Code == NotFound {
				out.Entries = append(out.Entries, DiffEntry{Path: p, Kind: "absent", Identity: "absent"})
				continue
			}
			return DiffSnapshot{}, e
		}
		info, e := parent.Lstat(name)
		if e != nil {
			closeParent()
			if errors.Is(e, os.ErrNotExist) {
				out.Entries = append(out.Entries, DiffEntry{Path: p, Kind: "absent", Identity: "absent"})
				continue
			}
			return DiffSnapshot{}, classify(e, NotFile)
		}
		entry := DiffEntry{Path: p, Size: info.Size(), Identity: fileRevision(ref.WorkspaceID, info)}
		switch {
		case info.Mode().IsRegular():
			entry.Kind = "regular"
			if info.Mode()&0100 != 0 {
				entry.Mode = 0100755
			} else {
				entry.Mode = 0100644
			}
			if info.Size() > int64(maxFileBytes) {
				entry.Unavailable = true
				entry.Limit = "file_bytes"
				closeParent()
				out.Entries = append(out.Entries, entry)
				continue
			}
			f, e := parent.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
			if e != nil {
				entry.Unavailable = true
				entry.Limit = "read_unavailable"
				closeParent()
				out.Entries = append(out.Entries, entry)
				continue
			}
			data, e := io.ReadAll(io.LimitReader(f, int64(maxFileBytes)+1))
			after, se := f.Stat()
			_ = f.Close()
			current, le := parent.Lstat(name)
			closeParent()
			if e != nil || se != nil || le != nil || fileRevision(ref.WorkspaceID, after) != entry.Identity || fileRevision(ref.WorkspaceID, current) != entry.Identity {
				return DiffSnapshot{}, &Error{Code: StaleFile, Message: "workspace file changed while it was read"}
			}
			if len(data) > maxFileBytes {
				entry.Unavailable = true
				entry.Limit = "file_bytes"
			} else if *aggregate+int64(len(data)) > 32<<20 {
				entry.Unavailable = true
				entry.Limit = "byte_limit"
			} else {
				entry.Data = data
				entry.Digest = sha256.Sum256(data)
				*aggregate += int64(len(data))
			}
			out.Entries = append(out.Entries, entry)
			continue
		case info.Mode()&os.ModeSymlink != 0:
			entry.Kind = "symlink"
			entry.Mode = 0120000
			target, e := parent.Readlink(name)
			current, le := parent.Lstat(name)
			closeParent()
			if e != nil || le != nil || fileRevision(ref.WorkspaceID, current) != entry.Identity {
				return DiffSnapshot{}, &Error{Code: StaleFile, Message: "workspace symlink changed while it was read"}
			}
			entry.Data = []byte(target)
			entry.Digest = sha256.Sum256(entry.Data)
		case info.IsDir():
			entry.Kind = "directory"
			closeParent()
		default:
			entry.Kind = "other"
			closeParent()
		}
		out.Entries = append(out.Entries, entry)
	}
	current, err := s.check(sessionID, cwd, expected)
	if err != nil {
		return DiffSnapshot{}, err
	}
	out.Workspace = current
	return out, nil
}
