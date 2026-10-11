// Command kit runs Kit's headless command line without the terminal client,
// so tests can exercise daemon lifecycle commands in subprocesses.
package main

import (
	"context"
	"os"

	"github.com/akonwi/kit/apps/cli/ffi/cli"
)

func main() {
	invocation := cli.Parse(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	if invocation.Kind != cli.InvocationExit {
		os.Stderr.WriteString("kit test binary: the terminal client is not built in\n")
		os.Exit(2)
	}
	os.Exit(invocation.ExitCode)
}
