package codingtools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

const (
	defaultBashTimeout = 120 * time.Second
	maxBashTimeout     = 24 * time.Hour
	maxBashOutputBytes = 30_000
)

type bashArgs struct {
	Command string `json:"command"`
	Timeout *int64 `json:"timeout,omitempty"`
}

// DirectBashResult is the shared low-level result used by direct composer
// execution and the model-facing bash tool.
type DirectBashResult = CommandExecution

// RunDirectBash executes one direct composer command with Kit's standard shell,
// timeout, and bounded output policy.
func RunDirectBash(ctx context.Context, command, cwd string) (DirectBashResult, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	return RunCommand(ctx, shell, command, cwd, defaultBashTimeout, maxBashOutputBytes)
}

type bashDetails struct {
	ExitCode     *int   `json:"exitCode,omitempty"`
	Truncated    bool   `json:"truncated"`
	TimedOut     bool   `json:"timedOut,omitempty"`
	OutputPath   string `json:"outputPath,omitempty"`
	OutputCapped bool   `json:"outputCapped,omitempty"`
}

func newBashTool(cwd string) droids.Tool[bashArgs] {
	return droids.Tool[bashArgs]{
		Name:        BashToolName,
		Description: "Run a shell command in the project directory. Use for file operations, building, testing, git, etc. Avoid interactive commands.",
		Parameters: objectSchema(map[string]any{
			"command": stringSchema("The shell command to run"),
			"timeout": numberSchema("Timeout in milliseconds (default 120000)"),
		}, "command"),
		Mode: droids.ModeSequential,
		Execute: func(ctx context.Context, args bashArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			if strings.TrimSpace(args.Command) == "" {
				return errorResult(fmt.Errorf("command is required"), bashDetails{}), nil
			}
			timeout := defaultBashTimeout
			if args.Timeout != nil {
				if *args.Timeout <= 0 {
					return errorResult(fmt.Errorf("timeout must be positive"), bashDetails{}), nil
				}
				if *args.Timeout > maxBashTimeout.Milliseconds() {
					return errorResult(fmt.Errorf("timeout must not exceed %d milliseconds", maxBashTimeout.Milliseconds()), bashDetails{}), nil
				}
				timeout = time.Duration(*args.Timeout) * time.Millisecond
			}

			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "bash"
			}
			execution, err := RunCommand(ctx, shell, args.Command, cwd, timeout, maxBashOutputBytes)
			if err != nil {
				if ctx.Err() != nil {
					return droids.ToolResult{}, ctx.Err()
				}
				return errorResult(err, bashDetails{}), nil
			}
			output := strings.TrimRight(execution.Output, "\r\n")
			if execution.TimedOut {
				if output != "" {
					output += "\n"
				}
				output += "[timed out]"
			}
			if execution.ExitCode != nil && *execution.ExitCode != 0 && !execution.TimedOut {
				output += fmt.Sprintf("\n[exit code: %d]", *execution.ExitCode)
			}
			if execution.Truncated {
				notice := "output truncated"
				if execution.OutputPath != "" {
					notice += " — captured output at " + execution.OutputPath
				}
				if execution.OutputCapped {
					notice += fmt.Sprintf(" (capture capped at %d MiB)", maxSpooledOutput>>20)
				}
				output += "\n[" + notice + "]"
			}
			return textResult(output, bashDetails{
				ExitCode: execution.ExitCode, Truncated: execution.Truncated,
				TimedOut: execution.TimedOut, OutputPath: execution.OutputPath,
				OutputCapped: execution.OutputCapped,
			}), nil
		},
	}
}
