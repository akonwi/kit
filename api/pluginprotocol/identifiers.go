package pluginprotocol

import (
	"encoding/json"
	"reflect"
)

// Version is the plugin protocol version this package describes.
const Version = 2

// ManifestVersion is the manifest format version that carries Version.
const ManifestVersion = 1

// SchemaURL and ManifestSchemaURL are the published $ids of the generated
// version 2 schemas.
const (
	SchemaURL         = "https://raw.githubusercontent.com/akonwi/kit/main/docs/plugin-protocol/v2/protocol.schema.json"
	ManifestSchemaURL = "https://raw.githubusercontent.com/akonwi/kit/main/docs/plugin-protocol/v2/manifest.schema.json"
)

// JSONRPCVersion is the value of every message's jsonrpc member.
const JSONRPCVersion = "2.0"

// FooterLocationID is the canonical ID of Kit's built-in cwd and Git footer
// item, the only built-in item a plugin may hide.
const FooterLocationID = "kit.footer.location"

// RequestID is a JSON-RPC request id: a string or an integer.
type RequestID json.RawMessage

// MarshalJSON emits the id verbatim.
func (id RequestID) MarshalJSON() ([]byte, error) {
	if len(id) == 0 {
		return []byte("null"), nil
	}
	return id, nil
}

// UnmarshalJSON retains the id verbatim, without numeric conversion.
func (id *RequestID) UnmarshalJSON(data []byte) error {
	*id = append((*id)[:0], data...)
	return nil
}

// JSONSchema describes a request id.
func (RequestID) JSONSchema() map[string]any {
	return map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}}
}

// PluginID is a plugin's stable identity from its manifest.
type PluginID string

// JSONSchema describes a plugin ID: lowercase, at most 32 characters, and
// neither kit nor kit-*.
func (PluginID) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 32, "pattern": "^[a-z][a-z0-9-]*$",
		"not": map[string]any{"pattern": "^kit(-|$)"}, "description": "Stable plugin ID. kit and kit-* are reserved."}
}

// LocalID identifies a plugin's command, subagent, or footer item: one or
// more dot-separated lowercase kebab-case segments, at most 128 bytes.
type LocalID string

// JSONSchema describes a local contribution ID.
func (LocalID) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 128, "pattern": `^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)*$`}
}

// ToolID identifies a plugin's tool: lowercase snake case, at most 64 bytes.
type ToolID string

// JSONSchema describes a local tool ID.
func (ToolID) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 64, "pattern": "^[a-z][a-z0-9_]*$"}
}

// CanonicalID is a contribution's plugin-qualified ID, <plugin>.<local>.
type CanonicalID string

// JSONSchema describes a canonical contribution ID.
func (CanonicalID) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 3, "pattern": `^[a-z][a-z0-9-]*\.[a-z][a-z0-9_-]*(?:\.[a-z][a-z0-9-]*)*$`}
}

// ModelToolName is the name a model calls a plugin tool by,
// <plugin>__<tool>, at most 64 characters.
type ModelToolName string

// JSONSchema describes a model-facing plugin tool name.
func (ModelToolName) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 4, "maxLength": 64, "pattern": "^[a-z][a-z0-9-]*__[a-z][a-z0-9_]*$"}
}

// FooterTarget names a footer item a plugin hides or shows: a plugin
// footer item's canonical ID or FooterLocationID.
type FooterTarget string

// JSONSchema describes a footer hide or show target.
func (FooterTarget) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "maxLength": 257, "pattern": `^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)*$`}
}

// IdempotencyKey deduplicates a plugin's session message.
type IdempotencyKey string

// JSONSchema describes an idempotency key.
func (IdempotencyKey) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[A-Za-z0-9._:-]{1,128}$"}
}

// Null is the result of a request that returns nothing.
type Null struct{}

// MarshalJSON emits null.
func (Null) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

// JSONSchema describes null.
func (Null) JSONSchema() map[string]any { return map[string]any{"type": "null"} }

var nullType = reflect.TypeFor[Null]()
