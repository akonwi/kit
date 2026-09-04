package tui

import (
	"fmt"
	"os/exec"
	"runtime"
)

func openExternalURL(raw string) error {
	if safeHTTPSHyperlink(raw) == "" {
		return fmt.Errorf("refusing to open an unsafe URL")
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", raw)
	case "linux":
		command = exec.Command("xdg-open", raw)
	default:
		return fmt.Errorf("opening URLs is unsupported on %s", runtime.GOOS)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start browser opener: %w", err)
	}
	go func() { _ = command.Wait() }()
	return nil
}
