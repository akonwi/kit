//go:build darwin

package serveridentity

import "testing"

func TestParseIOPlatformUUID(t *testing.T) {
	t.Parallel()
	output := []byte(`+-o J316sAP  <class IOPlatformExpertDevice, id 0x100000226, registered, matched, active, busy 0 (0 ms), retain 34>
    {
      "IOPlatformSerialNumber" = "XXXXXXXXXX"
      "IOPlatformUUID" = "4C1E7F7A-0000-4000-8000-1234567890AB"
    }`)
	got, err := parseIOPlatformUUID(output)
	if err != nil {
		t.Fatalf("parseIOPlatformUUID() error = %v", err)
	}
	if got != "4C1E7F7A-0000-4000-8000-1234567890AB" {
		t.Fatalf("parseIOPlatformUUID() = %q", got)
	}
	if _, err := parseIOPlatformUUID([]byte(`"IOPlatformSerialNumber" = "X"`)); err == nil {
		t.Fatal("parseIOPlatformUUID() accepted output without a UUID")
	}
}

func TestReadPlatformMachineIDOnThisMac(t *testing.T) {
	t.Parallel()
	if _, err := readPlatformMachineID(); err != nil {
		t.Fatalf("readPlatformMachineID() error = %v", err)
	}
}
