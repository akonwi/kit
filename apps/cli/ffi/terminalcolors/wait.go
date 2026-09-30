package terminalcolors

import (
	"time"

	"golang.org/x/sys/unix"
)

// waitReadable waits until fd has input. It uses select(2) because macOS
// cannot poll terminal devices.
func waitReadable(fd int, timeout time.Duration) (bool, error) {
	var readable unix.FdSet
	readable.Set(fd)
	interval := unix.NsecToTimeval(timeout.Nanoseconds())
	count, err := unix.Select(fd+1, &readable, nil, nil, &interval)
	if err != nil {
		return false, err
	}
	return count > 0 && readable.IsSet(fd), nil
}
