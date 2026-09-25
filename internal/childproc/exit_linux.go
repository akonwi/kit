package childproc

import (
	"errors"

	"golang.org/x/sys/unix"
)

// observeExit leaves the child waitable. Keeping the leader unreaped reserves
// its PID/PGID until the supervisor has finished signaling the entire group.
func observeExit(pid int) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

// Linux accepts signals to an unreaped zombie. EPERM is a real failure.
func groupHasOnlyExitedProcesses(int) bool { return false }
