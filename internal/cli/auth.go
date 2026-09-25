package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/auth"
	kitserver "github.com/akonwi/kit/internal/server"
)

const openAICodexDisplayName = "OpenAI Codex"

type openAICodexLogin interface {
	Login(context.Context, func(auth.OpenAICodexDeviceInstructions) error) error
}

func runLogin(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != auth.OpenAICodexProviderID {
		return loginUsage(stderr)
	}
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	manager := kitserver.NewManager(paths)
	// Fail before starting a potentially long browser interaction when a live
	// daemon would ignore the resulting stored credential.
	release, err := manager.AcquireCredentialStoreMutation(ctx, auth.OpenAICodexProviderID)
	if err != nil {
		return writeAuthFailure(ctx, stderr, "prepare OpenAI Codex login", err)
	}
	if err := release(); err != nil {
		return writeAuthFailure(ctx, stderr, "prepare OpenAI Codex login", err)
	}
	login, err := auth.NewOpenAICodexDeviceLogin(auth.OpenAICodexDeviceLoginOptions{
		Store: auth.NewStore(paths.Auth),
		AcquireSave: func(ctx context.Context) (func() error, error) {
			return manager.AcquireCredentialStoreMutation(ctx, auth.OpenAICodexProviderID)
		},
	})
	if err != nil {
		return writeAuthFailure(ctx, stderr, "configure OpenAI Codex login", err)
	}
	return executeOpenAICodexLogin(ctx, login, stdout, stderr)
}

func executeOpenAICodexLogin(
	ctx context.Context,
	login openAICodexLogin,
	stdout, stderr io.Writer,
) int {
	err := login.Login(ctx, func(instructions auth.OpenAICodexDeviceInstructions) error {
		_, err := fmt.Fprintf(
			stdout,
			"Open %s\nEnter code: %s\nWaiting for authorization…\n",
			instructions.VerificationURI,
			instructions.UserCode,
		)
		return err
	})
	if err != nil {
		return writeAuthFailure(ctx, stderr, "complete OpenAI Codex login", err)
	}
	fmt.Fprintln(stdout, "Logged in to OpenAI Codex.")
	return 0
}

func runLogout(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != auth.OpenAICodexProviderID {
		return logoutUsage(stderr)
	}
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	release, err := kitserver.NewManager(paths).AcquireCredentialStoreMutation(ctx, auth.OpenAICodexProviderID)
	if err != nil {
		return writeAuthFailure(ctx, stderr, "prepare OpenAI Codex logout", err)
	}
	deleteErr := auth.NewStore(paths.Auth).Delete(ctx, auth.OpenAICodexProviderID)
	releaseErr := release()
	if err := errors.Join(deleteErr, releaseErr); err != nil {
		return writeAuthFailure(ctx, stderr, "remove OpenAI Codex credentials", err)
	}
	fmt.Fprintln(stdout, "Logged out of OpenAI Codex.")
	return 0
}

func runAuthCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return authUsage(stderr)
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return authUsage(stderr)
		}
		paths, err := apphome.Resolve("")
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		entries, err := auth.NewStore(paths.Auth).List(ctx)
		if err != nil {
			return writeAuthFailure(ctx, stderr, "list provider credentials", err)
		}
		activeEnvironment, err := openAICodexEnvironmentActive(ctx, paths)
		if err != nil {
			return writeAuthFailure(ctx, stderr, "inspect active provider credentials", err)
		}
		if len(entries) == 0 && !activeEnvironment {
			fmt.Fprintln(stdout, "No saved provider credentials.")
			return 0
		}
		savedCodex := false
		for _, entry := range entries {
			fmt.Fprintf(stdout, "%s\t%s\tsaved\n", entry.ProviderID, entry.Type)
			savedCodex = savedCodex || entry.ProviderID == auth.OpenAICodexProviderID
		}
		if activeEnvironment {
			details := "active daemon"
			if savedCodex {
				details += "; overrides saved credential"
			}
			fmt.Fprintf(stdout, "%s\tenvironment\t%s\n", auth.OpenAICodexProviderID, details)
		}
		return 0
	case "login":
		return runLogin(ctx, args[1:], stdout, stderr)
	case "logout":
		return runLogout(ctx, args[1:], stdout, stderr)
	default:
		return authUsage(stderr)
	}
}

func openAICodexEnvironmentActive(ctx context.Context, paths apphome.Paths) (bool, error) {
	release, err := kitserver.NewManager(paths).AcquireCredentialStoreMutation(ctx, auth.OpenAICodexProviderID)
	if errors.Is(err, kitserver.ErrEnvironmentCredentialsActive) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, release()
}

func writeAuthFailure(ctx context.Context, output io.Writer, action string, err error) int {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			fmt.Fprintf(output, "kit: %s canceled\n", action)
			return 130
		}
		fmt.Fprintf(output, "kit: %s timed out\n", action)
		return 1
	}
	fmt.Fprintf(output, "kit: %s: %v\n", action, err)
	return 1
}

func loginUsage(output io.Writer) int {
	fmt.Fprintln(output, "usage: kit login openai-codex")
	return 2
}

func logoutUsage(output io.Writer) int {
	fmt.Fprintln(output, "usage: kit logout openai-codex")
	return 2
}

func authUsage(output io.Writer) int {
	fmt.Fprintln(output, "usage: kit auth <status|login openai-codex|logout openai-codex>")
	return 2
}
