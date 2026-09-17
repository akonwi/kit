package workingdiff

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// validateObjectStorage bounds and fingerprints every storage file Git may use
// for the requested object IDs. Object bytes remain identified by their OIDs;
// this check prevents Git from following object-store symlinks outside authority.
func validateObjectStorage(repo *repository, oids []string) ([32]byte, error) {
	h := sha256.New()
	packDir := filepath.Join(repo.common, "objects", "pack")
	if info, statErr := os.Lstat(packDir); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return [32]byte{}, unsupported("metadata_authority")
		}
		if bad, e := unsafeMeta(packDir); e != nil || bad {
			return [32]byte{}, unsupported("metadata_authority")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return [32]byte{}, unsupported("metadata_authority")
	}
	entries, err := os.ReadDir(packDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return [32]byte{}, unsupported("metadata_authority")
	}
	if len(entries) > 1024 {
		return [32]byte{}, unsupported("metadata_authority")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		allowed := name == "multi-pack-index" || strings.HasSuffix(name, ".pack") || strings.HasSuffix(name, ".idx") || strings.HasSuffix(name, ".rev") || strings.HasSuffix(name, ".bitmap") || strings.HasSuffix(name, ".mtimes")
		if !allowed {
			continue
		}
		if err := fingerprintObjectFile(h, filepath.Join(packDir, name)); err != nil {
			return [32]byte{}, err
		}
	}
	for _, oid := range oids {
		if !validOID(oid) {
			return [32]byte{}, repoUnavailable()
		}
		dir := filepath.Join(repo.common, "objects", oid[:2])
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return [32]byte{}, unsupported("metadata_authority")
		}
		if bad, e := unsafeMeta(dir); e != nil || bad {
			return [32]byte{}, unsupported("metadata_authority")
		}
		loose := filepath.Join(dir, oid[2:])
		if _, err = os.Lstat(loose); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return [32]byte{}, unsupported("metadata_authority")
		}
		if err := fingerprintObjectFile(h, loose); err != nil {
			return [32]byte{}, err
		}
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}
func fingerprintObjectFile(h interface{ Write([]byte) (int, error) }, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return unsupported("metadata_authority")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return unsupported("metadata_authority")
	}
	if bad, e := unsafeMeta(path); e != nil || bad {
		return unsupported("metadata_authority")
	}
	id, e := metadataIdentity(path)
	if e != nil {
		return unsupported("metadata_authority")
	}
	_, _ = fmt.Fprintf(h, "%s:%s:%d:%d\x00", path, id, info.Size(), info.ModTime().UnixNano())
	return nil
}
