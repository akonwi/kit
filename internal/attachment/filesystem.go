package attachment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/attachmentmeta"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/securefs"
)

const (
	manifestFilename = "manifest.json"
	ownersFilename   = "owners.json"
	contentFilename  = "content"
)

type Filesystem struct {
	root string
	now  func() time.Time
	mu   sync.Mutex
}

func NewFilesystem(root string) (*Filesystem, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("%w: empty storage root", ErrInvalidInput)
	}
	if err := securefs.MakePrivateDir(root); err != nil {
		return nil, fmt.Errorf("prepare attachment storage: %w", err)
	}
	return &Filesystem{root: filepath.Clean(root), now: time.Now}, nil
}

func (store *Filesystem) Put(ctx context.Context, input PutInput) (record Record, resultErr error) {
	if err := validatePutInput(input); err != nil {
		return Record{}, err
	}
	id, err := identifier.New(IDPrefix)
	if err != nil {
		return Record{}, err
	}
	temporary, err := os.MkdirTemp(store.root, ".put-")
	if err != nil {
		return Record{}, fmt.Errorf("create attachment staging directory: %w", err)
	}
	if err := securefs.ProtectDirectory(temporary); err != nil {
		_ = os.RemoveAll(temporary)
		return Record{}, fmt.Errorf("protect attachment staging directory: %w", err)
	}
	defer func() {
		if resultErr != nil && temporary != "" {
			_ = os.RemoveAll(temporary)
		}
	}()

	contentPath := filepath.Join(temporary, contentFilename)
	content, err := os.OpenFile(contentPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Record{}, fmt.Errorf("create attachment content: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(content, hash), io.LimitReader(contextReader{ctx: ctx, reader: input.Content}, input.MaxBytes+1))
	if copyErr == nil && written > input.MaxBytes {
		copyErr = ErrTooLarge
	}
	if copyErr == nil && input.Validate != nil {
		if _, seekErr := content.Seek(0, io.SeekStart); seekErr != nil {
			copyErr = fmt.Errorf("seek staged attachment: %w", seekErr)
		} else if validationErr := input.Validate(content); validationErr != nil {
			copyErr = fmt.Errorf("%w: content validation failed: %v", ErrInvalidInput, validationErr)
		}
	}
	if syncErr := content.Sync(); copyErr == nil && syncErr != nil {
		copyErr = syncErr
	}
	if closeErr := content.Close(); copyErr == nil && closeErr != nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		if errors.Is(copyErr, ErrTooLarge) || errors.Is(copyErr, ErrInvalidInput) {
			return Record{}, copyErr
		}
		return Record{}, fmt.Errorf("write attachment content: %w", copyErr)
	}

	record = Record{
		ID: id, SessionID: input.SessionID, Filename: input.Filename,
		MediaType: input.MediaType, Size: written, SHA256: hex.EncodeToString(hash.Sum(nil)),
		CreatedAt: store.now().UTC(), Width: input.Width, Height: input.Height,
	}
	if err := writeManifest(filepath.Join(temporary, manifestFilename), record); err != nil {
		return Record{}, err
	}
	if err := syncDirectory(temporary); err != nil {
		return Record{}, fmt.Errorf("sync attachment staging directory: %w", err)
	}
	finalPath := filepath.Join(store.root, id)
	if err := os.Rename(temporary, finalPath); err != nil {
		return Record{}, fmt.Errorf("publish attachment: %w", err)
	}
	temporary = ""
	if err := syncDirectory(store.root); err != nil {
		_ = os.RemoveAll(finalPath)
		_ = syncDirectory(store.root)
		return Record{}, fmt.Errorf("sync attachment storage: %w", err)
	}
	return record, nil
}

// Stat returns validated attachment metadata without reading attachment bytes.
func (store *Filesystem) Stat(ctx context.Context, sessionID, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if sessionID == "" || !identifier.Valid(id, IDPrefix) {
		return Record{}, ErrNotFound
	}
	directory := filepath.Join(store.root, id)
	manifestPath := filepath.Join(directory, manifestFilename)
	if err := requireRegularFile(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("validate attachment manifest: %w", err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("read attachment manifest: %w", err)
	}
	var record Record
	if err := json.Unmarshal(manifestBytes, &record); err != nil {
		return Record{}, fmt.Errorf("decode attachment manifest: %w", err)
	}
	if err := validateRecord(record); err != nil {
		return Record{}, fmt.Errorf("validate attachment manifest: %w", err)
	}
	if record.ID != id || !store.recordOwnedBy(record, sessionID) {
		return Record{}, ErrNotFound
	}
	contentPath := filepath.Join(directory, contentFilename)
	if err := requireRegularFile(contentPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("validate attachment content: %w", err)
	}
	contentInfo, err := os.Stat(contentPath)
	if err != nil {
		return Record{}, fmt.Errorf("stat attachment content: %w", err)
	}
	if contentInfo.Size() != record.Size {
		return Record{}, fmt.Errorf("attachment content size does not match manifest")
	}
	record.SessionID = sessionID
	return record, nil
}

func (store *Filesystem) Open(ctx context.Context, sessionID, id string) (Record, io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, nil, err
	}
	if sessionID == "" || !identifier.Valid(id, IDPrefix) {
		return Record{}, nil, ErrNotFound
	}
	directory := filepath.Join(store.root, id)
	manifestPath := filepath.Join(directory, manifestFilename)
	if err := requireRegularFile(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, nil, ErrNotFound
		}
		return Record{}, nil, fmt.Errorf("validate attachment manifest: %w", err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, nil, ErrNotFound
		}
		return Record{}, nil, fmt.Errorf("read attachment manifest: %w", err)
	}
	var record Record
	if err := json.Unmarshal(manifestBytes, &record); err != nil {
		return Record{}, nil, fmt.Errorf("decode attachment manifest: %w", err)
	}
	if err := validateRecord(record); err != nil {
		return Record{}, nil, fmt.Errorf("validate attachment manifest: %w", err)
	}
	if record.ID != id || !store.recordOwnedBy(record, sessionID) {
		return Record{}, nil, ErrNotFound
	}
	contentPath := filepath.Join(directory, contentFilename)
	if err := requireRegularFile(contentPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, nil, ErrNotFound
		}
		return Record{}, nil, fmt.Errorf("validate attachment content: %w", err)
	}
	content, err := os.Open(contentPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, nil, ErrNotFound
		}
		return Record{}, nil, fmt.Errorf("open attachment content: %w", err)
	}
	if err := verifyContent(content, record); err != nil {
		_ = content.Close()
		return Record{}, nil, err
	}
	record.SessionID = sessionID
	return record, content, nil
}

// Remove deletes one attachment ownership, removing bytes after the last owner.
func (store *Filesystem) Remove(ctx context.Context, sessionID, id string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.removeOwner(ctx, sessionID, id)
}

// RemoveSession deletes every readable attachment manifest owned by a session.
func (store *Filesystem) RemoveSession(ctx context.Context, sessionID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if sessionID == "" {
		return ErrInvalidInput
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return fmt.Errorf("list attachments: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || !identifier.Valid(entry.Name(), IDPrefix) {
			continue
		}
		manifestPath := filepath.Join(store.root, entry.Name(), manifestFilename)
		if err := requireRegularFile(manifestPath); err != nil {
			continue
		}
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return fmt.Errorf("read attachment manifest: %w", err)
		}
		var record Record
		if json.Unmarshal(data, &record) != nil || validateRecord(record) != nil || record.ID != entry.Name() || !store.recordOwnedBy(record, sessionID) {
			continue
		}
		if err := store.removeOwner(ctx, sessionID, record.ID); err != nil {
			return fmt.Errorf("remove session attachment: %w", err)
		}
	}
	return syncDirectory(store.root)
}

// ForkSession grants child ownership of all source attachments while preserving IDs.
func (store *Filesystem) ForkSession(ctx context.Context, sourceSessionID, childSessionID string) error {
	if sourceSessionID == "" || childSessionID == "" || sourceSessionID == childSessionID {
		return ErrInvalidInput
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return fmt.Errorf("list attachments: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || !identifier.Valid(entry.Name(), IDPrefix) {
			continue
		}
		manifestPath := filepath.Join(store.root, entry.Name(), manifestFilename)
		data, readErr := os.ReadFile(manifestPath)
		if readErr != nil {
			continue
		}
		var record Record
		if json.Unmarshal(data, &record) != nil || validateRecord(record) != nil || !store.recordOwnedBy(record, sourceSessionID) || store.recordOwnedBy(record, childSessionID) {
			continue
		}
		owners, err := store.readOwners(record.ID)
		if err != nil {
			return err
		}
		owners = append(owners, childSessionID)
		if err := store.writeOwners(record.ID, owners); err != nil {
			return err
		}
	}
	return syncDirectory(store.root)
}

func (store *Filesystem) removeOwner(ctx context.Context, sessionID, id string) error {
	if sessionID == "" || !identifier.Valid(id, IDPrefix) {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manifestPath := filepath.Join(store.root, id, manifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	var record Record
	if json.Unmarshal(data, &record) != nil || validateRecord(record) != nil || record.ID != id || !store.recordOwnedBy(record, sessionID) {
		return ErrNotFound
	}
	owners, err := store.readOwners(id)
	if err != nil {
		return err
	}
	if record.SessionID == sessionID && len(owners) == 0 {
		return os.RemoveAll(filepath.Join(store.root, id))
	}
	if record.SessionID == sessionID {
		record.SessionID = owners[0]
		owners = owners[1:]
		if err := replaceManifest(manifestPath, record); err != nil {
			return err
		}
	} else {
		kept := owners[:0]
		for _, owner := range owners {
			if owner != sessionID {
				kept = append(kept, owner)
			}
		}
		owners = kept
	}
	return store.writeOwners(id, owners)
}

func (store *Filesystem) recordOwnedBy(record Record, sessionID string) bool {
	if record.SessionID == sessionID {
		return true
	}
	owners, err := store.readOwners(record.ID)
	if err != nil {
		return false
	}
	for _, owner := range owners {
		if owner == sessionID {
			return true
		}
	}
	return false
}

func (store *Filesystem) readOwners(id string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(store.root, id, ownersFilename))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read attachment owners: %w", err)
	}
	var owners []string
	if err := json.Unmarshal(data, &owners); err != nil {
		return nil, fmt.Errorf("decode attachment owners: %w", err)
	}
	return owners, nil
}

func (store *Filesystem) writeOwners(id string, owners []string) error {
	path := filepath.Join(store.root, id, ownersFilename)
	if len(owners) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}
	data, err := json.Marshal(owners)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func replaceManifest(path string, record Record) error {
	temporary := path + ".tmp"
	_ = os.Remove(temporary)
	if err := writeManifest(temporary, record); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace attachment manifest: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func validatePutInput(input PutInput) error {
	if input.SessionID == "" || input.Content == nil || input.MaxBytes <= 0 || input.MaxBytes == math.MaxInt64 ||
		input.Width < 0 || input.Height < 0 || (input.Width == 0) != (input.Height == 0) || input.Width > 8192 || input.Height > 8192 {
		return ErrInvalidInput
	}
	if !attachmentmeta.ValidFilename(input.Filename) {
		return fmt.Errorf("%w: invalid filename", ErrInvalidInput)
	}
	mediaType, parameters, err := mime.ParseMediaType(input.MediaType)
	if err != nil || mediaType != input.MediaType || len(parameters) != 0 || !attachmentmeta.SupportedMediaType(mediaType) {
		return fmt.Errorf("%w: invalid media type", ErrInvalidInput)
	}
	if strings.HasPrefix(mediaType, "image/") != (input.Width > 0) {
		return fmt.Errorf("%w: dimensions do not match media type", ErrInvalidInput)
	}
	return nil
}

func validateRecord(record Record) error {
	if !identifier.Valid(record.ID, IDPrefix) || record.SessionID == "" || record.Size < 0 || record.CreatedAt.IsZero() {
		return ErrInvalidInput
	}
	if err := validatePutInput(PutInput{
		SessionID: record.SessionID, Filename: record.Filename, MediaType: record.MediaType,
		Content: strings.NewReader(""), MaxBytes: 1, Width: record.Width, Height: record.Height,
	}); err != nil {
		return err
	}
	checksum, err := hex.DecodeString(record.SHA256)
	if err != nil || len(checksum) != sha256.Size {
		return fmt.Errorf("%w: invalid checksum", ErrInvalidInput)
	}
	return nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("path is not a regular file")
	}
	return nil
}

func writeManifest(path string, record Record) error {
	contents, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode attachment manifest: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create attachment manifest: %w", err)
	}
	if _, err = file.Write(append(contents, '\n')); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write attachment manifest: %w", err)
	}
	return nil
}

func verifyContent(content *os.File, record Record) error {
	info, err := content.Stat()
	if err != nil {
		return fmt.Errorf("stat attachment content: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != record.Size {
		return errors.New("attachment content does not match manifest")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, content); err != nil {
		return fmt.Errorf("verify attachment content: %w", err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
		return errors.New("attachment content checksum mismatch")
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind attachment content: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
