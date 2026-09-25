package droids

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// tool.go — typed tools. Authors write a generic Tool[Args] with a strongly
// typed Execute; NewTool erases it into an AnyTool for the droid's tool list.
// The loop validates/decodes raw model arguments into Args before calling.

// ToolResultDelta is append-only content emitted while a tool is running.
type ToolResultDelta struct {
	Content []ResultContent
	IsError bool
}

// ToolUpdate publishes one append-only result delta. Tools opt into streaming
// by calling it synchronously and must not retain it after Execute returns.
type ToolUpdate func(ToolResultDelta)

// ToolResult is what a tool returns to the model.
type ToolResult struct {
	// Content is returned to the model (text and/or files).
	Content []ResultContent
	// Details is bounded canonical JSON for logs/UI, persisted with the tool
	// result but not sent to the model.
	Details json.RawMessage
	// IsError marks a completed tool result as an application-level failure while
	// preserving its content and details for the model and observers. Returning a
	// Go error remains appropriate for execution/transport failures.
	IsError bool
	// Terminate requests normal completion after the containing tool batch is
	// fully durable.
	Terminate bool
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

// ToolText is a convenience for a text-only tool result.
func ToolText(text string) ToolResult {
	return ToolResult{Content: []ResultContent{TextContent{Text: text}}}
}

// EncodeDetails converts structured tool details to canonical JSON.
func EncodeDetails(value any) (json.RawMessage, error) {
	return encodeDetails(value)
}

// Tool is a typed tool definition.
type Tool[Args any] struct {
	// RegistrationID fences dynamically owned callbacks across recovery. Owners
	// must change it whenever a registration is replaced; empty is for static tools.
	RegistrationID string
	Name           string
	Description    string
	// Parameters is the JSON Schema for Args. If nil, it is derived from Args
	// via reflection (json / jsonschema struct tags). Set it explicitly to
	// override derivation.
	Parameters map[string]any
	// Execute runs the tool with decoded arguments. Calling update opts the tool
	// into append-only streaming; tools that do not stream may ignore it. The
	// returned ToolResult remains the authoritative complete result. Return an
	// error to signal failure; the loop converts it into an error tool result.
	Execute func(ctx context.Context, call ToolContext, args Args, update ToolUpdate) (ToolResult, error)
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
	registrationID() string
	validate(raw []byte) error
	// execute decodes raw JSON args and runs the tool.
	execute(ctx context.Context, call ToolContext, raw []byte, update ToolUpdate) (ToolResult, error)
}

// NewTool validates and erases a typed Tool into an AnyTool.
func NewTool[Args any](t Tool[Args]) (AnyTool, error) {
	if t.Name == "" {
		return nil, fmt.Errorf("droids: tool name is required")
	}
	if t.Execute == nil {
		return nil, fmt.Errorf("droids: tool %q has no execute function", t.Name)
	}
	if t.Mode != ModeDefault && t.Mode != ModeSequential && t.Mode != ModeParallel {
		return nil, fmt.Errorf("droids: tool %q has invalid execution mode %q", t.Name, t.Mode)
	}
	parameters := t.Parameters
	if parameters == nil {
		parameters = deriveSchema[Args]()
	}
	canonical, compiled, err := compileToolSchema(t.Name, parameters)
	if err != nil {
		return nil, err
	}
	t.Parameters = canonical
	additional, specified := canonical["additionalProperties"].(bool)
	forbidAdditional := specified && !additional
	return boundTool[Args]{t: t, validator: compiled, forbidAdditional: forbidAdditional}, nil
}

// MustTool is NewTool for declarations where an invalid definition is a
// programmer error.
func MustTool[Args any](t Tool[Args]) AnyTool {
	tool, err := NewTool(t)
	if err != nil {
		panic(err)
	}
	return tool
}

type boundTool[Args any] struct {
	t                Tool[Args]
	validator        *validator.Schema
	forbidAdditional bool
}

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

func (b boundTool[Args]) mode() ExecutionMode    { return b.t.Mode }
func (b boundTool[Args]) registrationID() string { return b.t.RegistrationID }

func (b boundTool[Args]) validate(raw []byte) error {
	return validateToolArguments(raw, b.validator)
}

func compileToolSchema(name string, parameters map[string]any) (map[string]any, *validator.Schema, error) {
	raw, err := json.Marshal(parameters)
	if err != nil {
		return nil, nil, fmt.Errorf("droids: encode tool %q schema: %w", name, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var canonical map[string]any
	if err := decoder.Decode(&canonical); err != nil {
		return nil, nil, fmt.Errorf("droids: decode tool %q schema: %w", name, err)
	}
	if canonical["type"] != "object" {
		return nil, nil, fmt.Errorf("droids: tool %q parameters must be a JSON object schema", name)
	}
	if err := validateToolNumbers(canonical); err != nil {
		return nil, nil, err
	}
	compiler := validator.NewCompiler()
	const location = "urn:droids:tool-schema"
	if err := compiler.AddResource(location, canonical); err != nil {
		return nil, nil, fmt.Errorf("droids: load tool %q schema: %w", name, err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, nil, fmt.Errorf("droids: compile tool %q schema: %w", name, err)
	}
	return providerSchemaValue(canonical).(map[string]any), compiled, nil
}

// Provider SDKs can encode json.Number as a string. Use ordinary floats when
// their JSON spelling is lossless, and raw JSON numbers otherwise.
func providerSchemaValue(value any) any {
	switch value := value.(type) {
	case json.Number:
		number, err := value.Float64()
		encoded, encodeErr := json.Marshal(number)
		if err == nil && encodeErr == nil && string(encoded) == string(value) {
			return number
		}
		return json.RawMessage(value)
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, child := range value {
			result[key] = providerSchemaValue(child)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for index, child := range value {
			result[index] = providerSchemaValue(child)
		}
		return result
	default:
		return value
	}
}

// Bound arbitrary-precision arithmetic before the schema validator sees model
// numbers. A tiny exponent token must not trigger an enormous allocation.
func validateToolNumbers(value any) error {
	switch value := value.(type) {
	case json.Number:
		text := string(value)
		if len(text) > 1024 {
			return fmt.Errorf("tool JSON number exceeds precision budget")
		}
		if index := strings.IndexAny(text, "eE"); index >= 0 {
			exponent, err := strconv.Atoi(text[index+1:])
			if err != nil || exponent < -1024 || exponent > 1024 {
				return fmt.Errorf("tool JSON exponent exceeds precision budget")
			}
		}
	case map[string]any:
		for _, child := range value {
			if err := validateToolNumbers(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := validateToolNumbers(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateToolArguments(raw []byte, schema *validator.Schema) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("tool arguments are invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("tool arguments contain multiple JSON values")
		}
		return fmt.Errorf("tool arguments are invalid JSON: %w", err)
	}
	if err := validateToolNumbers(value); err != nil {
		return err
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("tool arguments do not match schema: %w", err)
	}
	return nil
}

func (b boundTool[Args]) execute(ctx context.Context, call ToolContext, raw []byte, update ToolUpdate) (ToolResult, error) {
	if err := b.validate(raw); err != nil {
		return ToolResult{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ToolResult{}, fmt.Errorf("tool arguments must be a JSON object")
	}
	var args Args
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if b.forbidAdditional {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(&args); err != nil {
		return ToolResult{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ToolResult{}, fmt.Errorf("tool arguments contain multiple JSON values")
		}
		return ToolResult{}, err
	}
	if update == nil {
		update = func(ToolResultDelta) {}
	}
	return b.t.Execute(ctx, call, args, update)
}
