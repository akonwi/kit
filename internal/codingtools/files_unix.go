//go:build darwin || linux

package codingtools

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const fileIOChunk = 64 << 10

type fileVersion struct {
	info    os.FileInfo
	size    int64
	modTime time.Time
}

func openDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("not a directory: %s", path)
	}
	return file, nil
}

func openRegularFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	return file, nil
}

func readRegularFile(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	body, _, err := readRegularFileVersion(ctx, path, maxBytes)
	return body, err
}

func readRegularFileVersion(ctx context.Context, path string, maxBytes int64) ([]byte, fileVersion, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return nil, fileVersion{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fileVersion{}, err
	}
	version := fileVersion{info: info, size: info.Size(), modTime: info.ModTime()}

	var body []byte
	buffer := make([]byte, fileIOChunk)
	for {
		if err := ctx.Err(); err != nil {
			return nil, fileVersion{}, err
		}
		read, readErr := file.Read(buffer)
		if read > 0 {
			if int64(len(body)+read) > maxBytes {
				return nil, fileVersion{}, fmt.Errorf("file exceeds %d MiB limit: %s", maxBytes>>20, path)
			}
			body = append(body, buffer[:read]...)
		}
		if readErr != nil {
			if readErr == io.EOF {
				return body, version, nil
			}
			return nil, fileVersion{}, readErr
		}
	}
}

func writeRegularFile(ctx context.Context, path string, content []byte, mode os.FileMode) error {
	return replaceRegularFile(ctx, path, content, mode, nil)
}

func replaceRegularFile(ctx context.Context, path string, content []byte, mode os.FileMode, expected *fileVersion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target, existing, err := regularWriteTarget(path)
	if err != nil {
		return err
	}
	if expected != nil {
		if existing == nil || !os.SameFile(expected.info, existing) || expected.size != existing.Size() || !expected.modTime.Equal(existing.ModTime()) {
			return fmt.Errorf("file changed while it was being edited: %s", path)
		}
	}
	preserveMode := existing != nil
	if preserveMode {
		if err := unix.Access(target, unix.W_OK); err != nil {
			return err
		}
		mode = existing.Mode().Perm()
	} else {
		mode = mode.Perm()
		if mode == 0 {
			mode = 0o600
		}
	}

	directory := filepath.Dir(target)
	createMode := mode
	if preserveMode {
		createMode = 0o600
	}
	temporary, err := createTemporaryFile(directory, createMode)
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	for len(content) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := content[:min(len(content), fileIOChunk)]
		written, err := temporary.Write(chunk)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	if preserveMode {
		if err := temporary.Chmod(mode); err != nil {
			return err
		}
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if expected != nil {
		current, err := os.Stat(target)
		if err != nil || !os.SameFile(expected.info, current) || expected.size != current.Size() || !expected.modTime.Equal(current.ModTime()) {
			return fmt.Errorf("file changed while it was being edited: %s", path)
		}
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return err
	}
	committed = true
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

func createTemporaryFile(directory string, mode os.FileMode) (*os.File, error) {
	for range 100 {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		path := filepath.Join(directory, fmt.Sprintf(".kit-write-%x", random[:]))
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("create temporary file in %s: too many collisions", directory)
}

func regularWriteTarget(path string) (string, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	target := path
	if info.Mode()&os.ModeSymlink != 0 {
		target, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", nil, err
		}
		info, err = os.Stat(target)
		if err != nil {
			return "", nil, err
		}
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("not a regular file: %s", path)
	}
	return target, info, nil
}
