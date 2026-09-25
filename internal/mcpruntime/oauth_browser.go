//go:build darwin || linux

package mcpruntime

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

func openOAuthBrowser(ctx context.Context, authorizationURL string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", authorizationURL)
	case "linux":
		command = exec.CommandContext(ctx, "xdg-open", authorizationURL)
	default:
		return fmt.Errorf("browser opening is unsupported on %s", runtime.GOOS)
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("open authorization URL: %w", err)
	}
	return nil
}
