package skills

import (
	"os"
	"testing"
)

// TestMain runs the package's tests as on a machine without Rex, unless a
// test gives a loader its own rex command.
func TestMain(m *testing.M) {
	findRexCommand = func() string { return "" }
	os.Exit(m.Run())
}
