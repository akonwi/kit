//go:build darwin || linux

package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/subagent"
	"github.com/akonwi/kit/internal/systemprompt"
)

// fixedBundleBuilder returns one prepared bundle, so a child runtime can be
// exercised without a live prompt, skill, or MCP stack.
type fixedBundleBuilder struct{ bundle RuntimeBundle }

func (b fixedBundleBuilder) Build(context.Context, SessionRecord, codingtools.CWDProvider) (RuntimeBundle, error) {
	return b.bundle, nil
}

// A child builder is configured without MCP. A child bundle that owns
// namespaces is a wiring mistake that would start duplicate server processes,
// so Open must refuse it rather than silently running them.
func TestChildRuntimeRejectsAChildOwnedMCPManager(t *testing.T) {
	cwd := mcpProject(t, `{"mcpServers":{"docs":{"command":"docs-server"}}}`)
	owner := buildMCPBundle(t, cwd)
	if owner.MCP == nil {
		t.Fatal("owner bundle has no MCP manager")
	}

	factory, err := NewChildRuntimeFactory(
		&mcpStubProviders{},
		fixedBundleBuilder{bundle: RuntimeBundle{Prompt: promptResult("child"), MCP: owner.MCP}},
		t.TempDir(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = factory.Open(t.Context(), subagent.Conversation{
		ID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OwnerSessionID: "session_1", CWD: cwd,
		Model: "anthropic/claude", Agent: subagent.Definition{Name: "reviewer", Description: "reviews"},
	})
	if err == nil || !strings.Contains(err.Error(), "owns MCP namespaces") {
		t.Fatalf("err = %v, want a rejection of child-owned namespaces", err)
	}
}

// A child borrows its owner's namespaces, so a failure to resolve them must
// surface rather than silently producing a child without MCP tools.
func TestChildRuntimeReportsBorrowFailure(t *testing.T) {
	factory, err := NewChildRuntimeFactory(
		&mcpStubProviders{},
		fixedBundleBuilder{bundle: RuntimeBundle{Prompt: promptResult("child")}},
		t.TempDir(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory.MCPTools = func(context.Context, string) ([]droids.AnyTool, error) {
		return nil, errBorrowFailed
	}

	_, err = factory.Open(t.Context(), subagent.Conversation{
		ID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OwnerSessionID: "session_1", CWD: t.TempDir(),
		Model: "anthropic/claude", Agent: subagent.Definition{Name: "reviewer", Description: "reviews"},
	})
	if err == nil || !strings.Contains(err.Error(), "borrow owner MCP tools") {
		t.Fatalf("err = %v, want the borrow failure to surface", err)
	}
}

// A child receives the owner's namespace tools in addition to its own.
func TestChildRuntimeAppendsBorrowedTools(t *testing.T) {
	cwd := mcpProject(t, `{"mcpServers":{"docs":{"command":"docs-server"}}}`)
	owner := buildMCPBundle(t, cwd)

	var requestedOwner string
	factory, err := NewChildRuntimeFactory(
		&mcpStubProviders{},
		fixedBundleBuilder{bundle: RuntimeBundle{Prompt: promptResult("child")}},
		t.TempDir(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory.MCPTools = func(_ context.Context, ownerSessionID string) ([]droids.AnyTool, error) {
		requestedOwner = ownerSessionID
		return owner.MCP.Tools(), nil
	}

	// Opening fails at model resolution, which happens after the borrow, so the
	// borrow is still observable without spawning a droid.
	_, _ = factory.Open(t.Context(), subagent.Conversation{
		ID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OwnerSessionID: "session_42", CWD: cwd,
		Model: "anthropic/claude", Agent: subagent.Definition{Name: "reviewer", Description: "reviews"},
	})

	if requestedOwner != "session_42" {
		t.Fatalf("borrowed from %q, want the conversation owner", requestedOwner)
	}
}

var (
	errBorrowFailed = errors.New("owner runtime is unavailable")
)

func promptResult(text string) systemprompt.Result {
	return systemprompt.Result{Prompt: text}
}

// mcpStubProviders fails model resolution, which occurs after bundle
// validation and the MCP borrow.
type mcpStubProviders struct{}

func (mcpStubProviders) Resolve(string) (droids.Model, error) {
	return droids.Model{}, errors.New("model resolution is not configured")
}

func (mcpStubProviders) Models() []droids.Model              { return nil }
func (mcpStubProviders) Model(string) (droids.Model, bool)   { return droids.Model{}, false }
func (mcpStubProviders) RefreshModels(context.Context) error { return nil }
func (mcpStubProviders) Stream(context.Context, droids.Model, droids.Request) droids.Stream {
	return nil
}
