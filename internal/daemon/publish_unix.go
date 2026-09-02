//go:build darwin || linux

package daemon

import "os"

// publishNoReplace atomically links a completed temporary inode into place and
// fails when a publication already exists.
func publishNoReplace(temporaryPath, destination string) error {
	if err := os.Link(temporaryPath, destination); err != nil {
		return err
	}
	_ = os.Remove(temporaryPath)
	return nil
}
