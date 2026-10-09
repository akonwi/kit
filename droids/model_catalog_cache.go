package droids

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// FileModelCatalogCache stores a model catalog in one local file.
type FileModelCatalogCache struct {
	Path string
}

// Load reads the cached catalog with the same size bound used for remote catalogs.
func (c FileModelCatalogCache) Load(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(c.Path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat model catalog cache: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("model catalog cache is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxModelCatalogSize+1))
	if err != nil {
		return nil, fmt.Errorf("read model catalog cache: %w", err)
	}
	if len(data) > maxModelCatalogSize {
		return nil, fmt.Errorf("model catalog cache exceeds 20 MiB")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

// Store atomically replaces the cached catalog using user-only permissions.
func (c FileModelCatalogCache) Store(ctx context.Context, data []byte) (err error) {
	if c.Path == "" {
		return fmt.Errorf("model catalog cache path is empty")
	}
	if len(data) > maxModelCatalogSize {
		return fmt.Errorf("model catalog cache exceeds 20 MiB")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(c.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create model catalog cache directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".models.dev-*.tmp")
	if err != nil {
		return fmt.Errorf("create model catalog cache temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect model catalog cache temporary file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write model catalog cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync model catalog cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close model catalog cache: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, c.Path); err != nil {
		return fmt.Errorf("replace model catalog cache: %w", err)
	}
	return nil
}
