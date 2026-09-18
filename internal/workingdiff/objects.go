package workingdiff

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
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
func matchesObjectOID(kind string, content []byte, oid string) bool {
	header := []byte(fmt.Sprintf("%s %d%c", kind, len(content), 0))
	var digest []byte
	switch len(oid) {
	case 40:
		h := sha1.New()
		_, _ = h.Write(header)
		_, _ = h.Write(content)
		digest = h.Sum(nil)
	case 64:
		h := sha256.New()
		_, _ = h.Write(header)
		_, _ = h.Write(content)
		digest = h.Sum(nil)
	default:
		return false
	}
	return hex.EncodeToString(digest) == oid
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

// validateObjectStoreAuthority rejects unsafe loose-object entries and returns
// a bounded metadata snapshot covering all local object storage Git may read
// while traversing commit ancestry.
func validateObjectStoreAuthority(repo *repository) ([32]byte, error) {
	h := sha256.New()
	packDigest, err := validateObjectStorage(repo, nil)
	if err != nil {
		return [32]byte{}, err
	}
	_, _ = h.Write(packDigest[:])
	objectsRoot := filepath.Join(repo.common, "objects")
	directories, err := os.ReadDir(objectsRoot)
	if err != nil {
		return [32]byte{}, unsupported("metadata_authority")
	}
	count := 0
	for _, directory := range directories {
		name := directory.Name()
		if len(name) != 2 || !isLowerHex(name) {
			continue
		}
		path := filepath.Join(objectsRoot, name)
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return [32]byte{}, unsupported("metadata_authority")
		}
		if bad, err := unsafeMeta(path); err != nil || bad {
			return [32]byte{}, unsupported("metadata_authority")
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return [32]byte{}, unsupported("metadata_authority")
		}
		for _, entry := range entries {
			if !isLowerHex(entry.Name()) {
				continue
			}
			count++
			if count > 100_000 {
				return [32]byte{}, &Error{Code: LimitExceeded, Message: "loose object store exceeds validation limit", Details: map[string]string{"limit": "candidate_limit"}}
			}
			if err := fingerprintObjectFile(h, filepath.Join(path, entry.Name())); err != nil {
				return [32]byte{}, err
			}
		}
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result, nil
}

func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
