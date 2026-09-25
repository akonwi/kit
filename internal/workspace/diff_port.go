package workspace

import (
	"context"
	"crypto/sha1" // #nosec G505 -- Git SHA-1 object identity, not a security primitive.
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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
	GitSHA1              string
	GitSHA256            string
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
	resolver := diffPathResolver{directories: make(map[string]map[string]struct{})}
	var hashedBytes int64
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return DiffSnapshot{}, err
		}
		if protocol.ValidateWorkspacePath(p, false) != nil {
			return DiffSnapshot{}, &Error{Code: InvalidPath, Message: "workspace path is invalid"}
		}
		parent, name, closeParent, e := resolver.openParent(root, p)
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
				if hashedBytes+info.Size() > 256<<20 {
					entry.Limit = "byte_limit"
					closeParent()
					out.Entries = append(out.Entries, entry)
					continue
				}
				f, openErr := parent.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
				if openErr != nil {
					entry.Limit = "read_unavailable"
					closeParent()
					out.Entries = append(out.Entries, entry)
					continue
				}
				contentHash := sha256.New()
				gitSHA1, gitSHA256 := sha1.New(), sha256.New() // #nosec G401 -- both Git object formats are supported.
				header := []byte(fmt.Sprintf("blob %d\x00", info.Size()))
				_, _ = gitSHA1.Write(header)
				_, _ = gitSHA256.Write(header)
				readBytes, readErr := io.Copy(io.MultiWriter(contentHash, gitSHA1, gitSHA256), io.LimitReader(f, info.Size()+1))
				after, statErr := f.Stat()
				_ = f.Close()
				current, lstatErr := parent.Lstat(name)
				closeParent()
				if readErr != nil || statErr != nil || lstatErr != nil || readBytes != info.Size() || fileRevision(ref.WorkspaceID, after) != entry.Identity || fileRevision(ref.WorkspaceID, current) != entry.Identity {
					return DiffSnapshot{}, &Error{Code: StaleFile, Message: "workspace file changed while it was read"}
				}
				copy(entry.Digest[:], contentHash.Sum(nil))
				entry.GitSHA1 = hex.EncodeToString(gitSHA1.Sum(nil))
				entry.GitSHA256 = hex.EncodeToString(gitSHA256.Sum(nil))
				hashedBytes += info.Size()
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

const maxDiffPathNameChecks = 20_000

type diffPathResolver struct {
	directories map[string]map[string]struct{}
	checked     int
}

func (r *diffPathResolver) exactChild(root *os.Root, directory, name string) (os.FileInfo, error) {
	names, ok := r.directories[directory]
	if !ok {
		remaining := maxDiffPathNameChecks - r.checked
		if remaining <= 0 {
			return nil, &Error{Code: LimitExceeded, Message: "workspace path name checks exceeded limit", Details: map[string]string{"limit": "path_name_checks"}}
		}
		file, err := root.Open(".")
		if err != nil {
			return nil, classify(err, NotFound)
		}
		entries, readErr := file.Readdirnames(remaining + 1)
		_ = file.Close()
		if readErr != nil && readErr != io.EOF {
			return nil, classify(readErr, NotFound)
		}
		if len(entries) > remaining {
			return nil, &Error{Code: LimitExceeded, Message: "workspace path name checks exceeded limit", Details: map[string]string{"limit": "path_name_checks"}}
		}
		r.checked += len(entries)
		names = make(map[string]struct{}, len(entries))
		for _, entry := range entries {
			names[entry] = struct{}{}
		}
		r.directories[directory] = names
	}
	if _, ok := names[name]; !ok {
		return nil, &Error{Code: NotFound, Message: "workspace path does not exist"}
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, classify(err, NotFound)
	}
	return info, nil
}

func (r *diffPathResolver) openParent(root *os.Root, name string) (*os.Root, string, func(), error) {
	parts := strings.Split(name, "/")
	current := root
	directory := ""
	opened := make([]*os.Root, 0, len(parts)-1)
	cleanup := func() {
		for index := len(opened) - 1; index >= 0; index-- {
			_ = opened[index].Close()
		}
	}
	for _, component := range parts[:len(parts)-1] {
		before, err := r.exactChild(current, directory, component)
		if err != nil {
			cleanup()
			return nil, "", func() {}, err
		}
		if before.Mode()&os.ModeSymlink != 0 {
			cleanup()
			return nil, "", func() {}, &Error{Code: SymlinkTraversal, Message: "workspace path traverses a symbolic link"}
		}
		if !before.IsDir() {
			cleanup()
			return nil, "", func() {}, &Error{Code: NotDirectory, Message: "workspace path is not a directory"}
		}
		next, err := current.OpenRoot(component)
		if err != nil {
			cleanup()
			return nil, "", func() {}, classify(err, NotDirectory)
		}
		handle, err := next.Open(".")
		if err != nil {
			_ = next.Close()
			cleanup()
			return nil, "", func() {}, classify(err, NotDirectory)
		}
		after, statErr := handle.Stat()
		_ = handle.Close()
		if statErr != nil || !os.SameFile(before, after) {
			_ = next.Close()
			cleanup()
			return nil, "", func() {}, &Error{Code: SymlinkTraversal, Message: "workspace directory changed during resolution"}
		}
		opened = append(opened, next)
		current = next
		if directory == "" {
			directory = component
		} else {
			directory += "/" + component
		}
	}
	final := parts[len(parts)-1]
	if _, err := r.exactChild(current, directory, final); err != nil {
		cleanup()
		return nil, "", func() {}, err
	}
	return current, final, cleanup, nil
}
