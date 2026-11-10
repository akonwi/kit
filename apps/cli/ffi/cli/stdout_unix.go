//go:build unix

package cli

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// reserveStdout keeps the process's stdout for protocol messages only: it
// returns a private copy of stdout and points descriptor 1 at stderr, so
// anything else that writes to stdout, in this process or a child, writes
// to stderr instead.
func reserveStdout() (*os.File, error) {
	stdout, stderr := int(os.Stdout.Fd()), int(os.Stderr.Fd())
	private, err := unix.Dup(stdout)
	if err != nil {
		return nil, fmt.Errorf("reserve stdout: %w", err)
	}
	unix.CloseOnExec(private)
	if err := unix.Dup2(stderr, stdout); err != nil {
		_ = unix.Close(private)
		return nil, fmt.Errorf("redirect stdout to stderr: %w", err)
	}
	return os.NewFile(uintptr(private), "protocol stdout"), nil
}
