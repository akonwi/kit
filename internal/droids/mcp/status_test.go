package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestManagerStatusesProjectSafeLifecycleAndSavedCredentialBoolean(t *testing.T) {
	manager, err := NewManager(Server{
		Name: "docs",
		Transport: func(context.Context) (sdkmcp.Transport, error) {
			return nil, errors.New("Authorization: Bearer secret-token")
		},
		OAuthSaved: func(context.Context) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	before := manager.Statuses(t.Context())
	if len(before) != 1 || before[0].State != "configured" || !before[0].OAuthSaved || before[0].LastError != "" {
		t.Fatalf("initial status = %#v", before)
	}
	_, _ = manager.namespaces[0].execute(t.Context(), namespaceRequest{Action: "list"})
	after := manager.Statuses(t.Context())
	if after[0].State != "error" || !strings.Contains(after[0].LastError, "secret-token") {
		t.Fatalf("runtime status = %#v", after)
	}
	// The core status intentionally retains the private error for server-side
	// diagnostics. Session/protocol projection replaces it with a safe message.
}
