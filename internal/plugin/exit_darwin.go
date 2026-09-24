package plugin

import (
	"errors"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// observeExit uses a process-exit event without reaping the child. Its PID/PGID
// remains reserved until the supervisor finishes signaling the process group.
func observeExit(pid int) error {
	syscall.ForkLock.RLock()
	queue, err := unix.Kqueue()
	if err == nil {
		unix.CloseOnExec(queue)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return err
	}
	defer unix.Close(queue)
	changes := []unix.Kevent_t{{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}}
	events := make([]unix.Kevent_t, 1)
	for {
		count, err := unix.Kevent(queue, changes, events, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		// A child that exited before registration can no longer be watched. It is
		// still ours and unreaped: only the supervisor ever calls command.Wait.
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		if err != nil {
			return err
		}
		if count == 0 {
			continue
		}
		if events[0].Flags&unix.EV_ERROR != 0 {
			err := syscall.Errno(events[0].Data)
			if err == 0 || errors.Is(err, unix.ESRCH) {
				return nil
			}
			return err
		}
		return nil
	}
}

// Darwin kill(2) returns EPERM, rather than ESRCH, for groups containing only
// zombies. Confirm that state before treating a failed signal as settled; do
// not suppress a real permission failure involving a live descendant.
func groupHasOnlyExitedProcesses(pid int) bool {
	const (
		zombie        = 5      // SZOMB from <sys/proc.h>.
		workingOnExit = 0x2000 // P_WEXIT from <sys/proc.h>.
	)
	// kill can observe exit before kern.proc reports SZOMB. Wait briefly only
	// when every non-zombie member is already exiting. P_WEXIT alone never
	// suppresses a signal error; a fresh observation must confirm no live member.
	deadline := time.Now().Add(20 * time.Millisecond)
	for {
		processes, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pid)
		if err != nil {
			return false
		}
		exiting := false
		for _, process := range processes {
			if process.Proc.P_stat == zombie {
				continue
			}
			if process.Proc.P_flag&workingOnExit == 0 {
				return false
			}
			exiting = true
		}
		if !exiting {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}
