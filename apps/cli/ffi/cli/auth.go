package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
)

// The provider logins `kit auth` runs against this home's server, which it
// starts when needed. The server runs each login and stores the credential.
// `kit auth status` only reads, so it reports on a running server and never
// starts or replaces one.

// connectAuthServer connects to this home's server, starting it as `kit`
// does.
func connectAuthServer(ctx context.Context) (*kit.Client, error) {
	paths, err := apphome.Resolve("")
	if err != nil {
		return nil, err
	}
	return bootstrapLocalClient(ctx, paths)
}

func runAuthCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return authUsage(stderr)
	}
	switch {
	case args[0] == "status" && len(args) == 1:
		return runAuthStatus(ctx, stdout, stderr)
	case args[0] == "login" && len(args) == 2 && args[1] == deviceLoginProvider:
		client, err := connectAuthServer(ctx)
		if err != nil {
			return writeAuthFailure(ctx, stderr, "connect to server", err)
		}
		defer client.Close()
		return executeOpenAICodexLogin(ctx, serverDeviceLogin{client: client}, stdout, stderr)
	case args[0] == "logout" && len(args) == 2:
		return runLogout(ctx, args[1], stdout, stderr)
	default:
		return authUsage(stderr)
	}
}

func runAuthStatus(ctx context.Context, stdout, stderr io.Writer) int {
	client, _, err := connectOwnServer(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	defer client.Close()
	listContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	providers, err := client.Providers(listContext)
	if err != nil {
		return writeAuthFailure(ctx, stderr, "list providers", loginError(err))
	}
	table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, provider := range providers {
		status := "not logged in"
		switch {
		case provider.Configured && provider.Source == kit.CredentialSourceEnvironment:
			status = "logged in from the server's environment"
		case provider.Configured:
			status = "logged in"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\n", provider.ID, provider.Name, status)
	}
	if err := table.Flush(); err != nil {
		return writeAuthFailure(ctx, stderr, "write provider status", err)
	}
	return 0
}

func executeOpenAICodexLogin(ctx context.Context, login DeviceLogin, stdout, stderr io.Writer) int {
	err := login.Login(ctx, func(instructions DeviceLoginInstructions) error {
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

func runLogout(ctx context.Context, providerID string, stdout, stderr io.Writer) int {
	client, err := connectAuthServer(ctx)
	if err != nil {
		return writeAuthFailure(ctx, stderr, "connect to server", err)
	}
	defer client.Close()
	logoutContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Logout(logoutContext, providerID); err != nil {
		var failure *kit.ServerError
		if errors.As(err, &failure) && failure.Code == kit.ErrorNotFound {
			fmt.Fprintf(stderr, "kit: unknown provider %q; list providers with `kit auth status`\n", providerID)
			return 2
		}
		return writeAuthFailure(ctx, stderr, "log out of "+providerID, loginError(err))
	}
	fmt.Fprintf(stdout, "Logged out of %s.\n", providerID)
	return 0
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

func authUsage(output io.Writer) int {
	fmt.Fprintln(output, "usage: kit auth <status|login openai-codex|logout PROVIDER>")
	return 2
}
