//go:build darwin || linux

// Package securefs applies user-only filesystem permissions on macOS and Linux.
package securefs

import "os"

// MakePrivateDir creates a directory tree without granting group or world access.
func MakePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return ProtectDirectory(path)
}

// ProtectDirectory restricts an existing directory to its owner.
func ProtectDirectory(path string) error {
	return os.Chmod(path, 0o700)
}

// ProtectFile restricts an existing file to its owner.
func ProtectFile(path string) error {
	return os.Chmod(path, 0o600)
}
