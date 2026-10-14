//go:build linux

package serveridentity

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var machineIDFiles = []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}

// readPlatformMachineID reads the systemd or D-Bus machine identifier.
func readPlatformMachineID() (string, error) {
	var failures error
	for _, path := range machineIDFiles {
		body, err := os.ReadFile(path)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if value, ok := parseMachineIDFile(body); ok {
			return value, nil
		}
		failures = errors.Join(failures, fmt.Errorf("%s is malformed", path))
	}
	return "", fmt.Errorf("read machine id: %w", failures)
}

func parseMachineIDFile(body []byte) (string, bool) {
	value := strings.TrimSpace(string(body))
	if len(value) != 32 || strings.Trim(strings.ToLower(value), "0123456789abcdef") != "" {
		return "", false
	}
	// systemd writes "uninitialized" or all zeros before first boot completes.
	if strings.Trim(value, "0") == "" {
		return "", false
	}
	return strings.ToLower(value), true
}
