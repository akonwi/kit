//go:build darwin

package serveridentity

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"time"
)

var platformUUIDPattern = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([0-9A-Fa-f-]{36})"`)

// readPlatformMachineID reads the hardware UUID from the IOKit registry
// through ioreg, keeping the build CGO-free.
func readPlatformMachineID() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", fmt.Errorf("read IOPlatformUUID: %w", err)
	}
	return parseIOPlatformUUID(output)
}

func parseIOPlatformUUID(output []byte) (string, error) {
	match := platformUUIDPattern.FindSubmatch(output)
	if match == nil {
		return "", fmt.Errorf("IOPlatformUUID not found")
	}
	return string(match[1]), nil
}
