//go:build !unix

package cli

import "os"

// reserveStdout leaves stdout as it is where descriptors cannot be moved;
// kit acp then relies on writing nothing else to stdout.
func reserveStdout() (*os.File, error) { return nil, nil }
