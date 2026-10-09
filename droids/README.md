# Droids

Droids is an application-neutral Go SDK for building autonomous model agents.
A droid owns its conversation state, model/tool loop, retries, compaction,
persistence coordination, and execution lifecycle.

## Install

```sh
go get github.com/akonwi/kit/droids
```

Droids requires Go 1.26 or newer.

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/akonwi/kit/droids"
)

func main() {
	ctx := context.Background()

	providers, err := droids.NewProviders(droids.OpenAI{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}
	model, err := providers.Resolve("openai/gpt-4.1-mini")
	if err != nil {
		log.Fatal(err)
	}

	agent, err := droids.Spawn(ctx, "example-conversation", droids.Config{
		Model:        model,
		SystemPrompt: "Be concise and helpful.",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()

	execution, err := agent.Prompt(ctx, droids.Input{
		Content: []droids.InputContent{
			droids.TextInput{Text: "Explain context cancellation in Go."},
		},
	}, droids.PromptOptions{})
	if err != nil {
		log.Fatal(err)
	}

	outcome, err := execution.Wait(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("status: %s\nmessage: %#v\n", outcome.Status, outcome.FinalMessage)
}
```

When no Store is supplied, each droid uses an in-memory store.

## Add a typed tool

Tool argument schemas are derived from Go types and `jsonschema` tags. Explicit
JSON Schema can also be supplied through `Tool.Parameters`.

```go
type weatherArgs struct {
	City string `json:"city" jsonschema:"required,description=city name"`
}

weather := droids.MustTool(droids.Tool[weatherArgs]{
	Name:        "weather",
	Description: "Get the current weather for a city.",
	Execute: func(
		ctx context.Context,
		toolCtx droids.ToolContext,
		args weatherArgs,
		update droids.ToolUpdate,
	) (droids.ToolResult, error) {
		return droids.ToolText("Sunny in " + args.City), nil
	},
})

agent, err := droids.Spawn(ctx, "tool-example", droids.Config{
	Model: model,
	Tools: []droids.AnyTool{weather},
})
```

A tool call is durably admitted before execution. Hooks can enforce policy or
transform results with `Config.BeforeToolCall` and `Config.AfterToolCall`.

## Durable SQLite conversations

The SQLite adapter is CGO-free and stores one conversation per database file.
Each live droid must receive its own Store instance.

```go
import "github.com/akonwi/kit/droids/sqlitestore"

store, err := sqlitestore.Open(ctx, sqlitestore.Options{
	Path: "./conversations/example.sqlite",
})
if err != nil {
	log.Fatal(err)
}
defer store.Close()

agent, err := droids.Spawn(ctx, "durable-example", droids.Config{
	Store: store,
	Model: model,
})
```

Calling `Spawn` again with the same conversation ID and Store restores its
state. Interrupted work is restored as recoverable rather than resumed without
application supervision.

## Package layout

```text
droids                 runtime, domain types, providers, tools, MemoryStore
droids/sqlitestore     CGO-free SQLite Store and migrations
droids/droidstest      reusable Store conformance support
droids/mcp             MCP namespace tools and transport contracts
droids/anthropicoauth  Anthropic OAuth protocol helpers
droids/openaicodex     OpenAI Codex OAuth protocol helpers
```

Packages below `droids/internal` are implementation details.

## Runtime model

```text
Storage      durable conversation and event boundary
Droid        autonomous model/tool loop and lifecycle
Providers    model discovery, routing, and wire translation
```

After `Prompt` admits input, the droid runs until it completes, fails, pauses,
is aborted, or becomes recoverable. Applications observe execution through the
returned handle, snapshots, and event subscriptions; they do not drive
individual model or tool cycles.

The SDK includes providers for OpenAI Responses, OpenAI Codex, Anthropic,
Cloudflare AI Gateway, and OpenCode Go. Custom providers can implement
`Provider` directly or use `AdaptProvider`.

The module is pre-1.0. Consumers should pin a version when depending on its
public API.
