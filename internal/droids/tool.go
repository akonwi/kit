package droids

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/invopop/jsonschema"
)

// tool.go — typed tools. Authors write a generic Tool[Args] with a strongly
// typed Execute; NewTool erases it into an AnyTool for the droid's tool list.
// The loop validates/decodes raw model arguments into Args before calling.

// ToolResultDelta is append-only content emitted while a tool is running.
type ToolResultDelta struct {
	Content []Content
	IsError bool
}

// ToolUpdate publishes one append-only result delta. Tools opt into streaming
// by calling it synchronously and must not retain it after Execute returns.
type ToolUpdate func(ToolResultDelta)

// ToolResult is what a tool returns to the model.
type ToolResult struct {
	// Content is returned to the model (text and/or images).
	Content []Content
	// Details is arbitrary structured data for logs/UI, persisted with the
	// tool result but not sent to the model.
	Details any
	// IsError marks a completed tool result as an application-level failure while
	// preserving its content and details for the model and observers. Returning a
	// Go error remains appropriate for execution/transport failures.
	IsError bool
}

// BeforeToolContext is passed to a BeforeToolCall hook.
type BeforeToolContext struct {
	ToolCall ToolCall
	// Args is the raw JSON arguments the model produced.
	Args []byte
}

// BeforeToolResult is returned by a BeforeToolCall hook. Zero value = proceed.
type BeforeToolResult struct {
	// Reject prevents this tool call from executing and appends an error tool
	// result containing Reason. The model observes the rejection and the run
	// continues normally; Reject does not halt or abort the run.
	Reject bool
	// Reason is the error text when Reject is set. Defaults to a generic message.
	Reason string
	// Result short-circuits execution: when non-nil (and not rejected), the
	// tool is skipped and this result is used as-is. The idempotency primitive
	// — return a previously stored result keyed by ToolCall.ID.
	Result *ToolResult
}

// AfterToolContext is passed to an AfterToolCall hook.
type AfterToolContext struct {
	ToolCall ToolCall
	Result   ToolResult
	IsError  bool
}

// ToolText is a convenience for a text-only tool result.
func ToolText(text string) ToolResult {
	return ToolResult{Content: []Content{TextContent{Text: text}}}
}

// Tool is a typed tool definition.
type Tool[Args any] struct {
	Name        string
	Description string
	// Parameters is the JSON Schema for Args. If nil, it is derived from Args
	// via reflection (json / jsonschema struct tags). Set it explicitly to
	// override derivation.
	Parameters map[string]any
	// Execute runs the tool with decoded arguments. Calling update opts the tool
	// into append-only streaming; tools that do not stream may ignore it. The
	// returned ToolResult remains the authoritative complete result. Return an
	// error to signal failure; the loop converts it into an error tool result.
	Execute func(ctx context.Context, args Args, update ToolUpdate) (ToolResult, error)
	// Mode overrides execution mode for this tool ("sequential" | "parallel").
	Mode ExecutionMode
}

// ExecutionMode controls whether a tool batch runs sequentially or in parallel.
type ExecutionMode string

const (
	ModeDefault    ExecutionMode = ""
	ModeSequential ExecutionMode = "sequential"
	ModeParallel   ExecutionMode = "parallel"
)

// AnyTool is the type-erased tool the runtime works with.
type AnyTool interface {
	schema() ToolSchema
	mode() ExecutionMode
	// execute decodes raw JSON args and runs the tool.
	execute(ctx context.Context, raw []byte, update ToolUpdate) (ToolResult, error)
}

// NewTool erases a typed Tool[Args] into an AnyTool.
func NewTool[Args any](t Tool[Args]) AnyTool { return boundTool[Args]{t} }

type boundTool[Args any] struct{ t Tool[Args] }

func (b boundTool[Args]) schema() ToolSchema {
	params := b.t.Parameters
	if params == nil {
		params = deriveSchema[Args]()
	}
	return ToolSchema{
		Name:        b.t.Name,
		Description: b.t.Description,
		Parameters:  params,
	}
}

// deriveSchema reflects Args into a flat JSON Schema object suitable for a
// provider tool definition. Definitions are inlined (no $ref/$defs) and the
// $schema meta key is dropped, since providers expect a plain object schema.
func deriveSchema[Args any]() map[string]any {
	reflector := jsonschema.Reflector{
		DoNotReference: true, // inline everything; providers reject $ref
		ExpandedStruct: true, // top level is the object itself
	}
	var zero Args
	schema := reflector.Reflect(zero)

	raw, err := json.Marshal(schema)
	if err != nil {
		return emptyObjectSchema()
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return emptyObjectSchema()
	}
	delete(out, "$schema")
	delete(out, "$id")
	if _, ok := out["type"]; !ok {
		// Non-struct Args (e.g. struct{}) may not reflect to an object.
		return emptyObjectSchema()
	}
	return out
}

func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (b boundTool[Args]) mode() ExecutionMode { return b.t.Mode }

func (b boundTool[Args]) execute(ctx context.Context, raw []byte, update ToolUpdate) (ToolResult, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ToolResult{}, fmt.Errorf("tool arguments must be a JSON object")
	}
	var args Args
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return ToolResult{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ToolResult{}, fmt.Errorf("tool arguments contain multiple JSON values")
		}
		return ToolResult{}, err
	}
	if b.t.Execute == nil {
		return ToolResult{}, fmt.Errorf("tool %q has no execute function", b.t.Name)
	}
	if update == nil {
		update = func(ToolResultDelta) {}
	}
	return b.t.Execute(ctx, args, update)
}
