package pluginprotocol

import "encoding/json"

// CommandRegisterParams registers a command users invoke from a client's
// command palette.
type CommandRegisterParams struct {
	ID          string  `json:"id" schema:"def=LocalID"`
	Description string  `json:"description" schema:"minLength=1,maxLength=1024,text=line-nonblank" doc:"Single-line, at most 1,024 bytes."`
	ArgName     *string `json:"argName,omitempty" schema:"nullable,maxLength=128,text=line" doc:"Hint for the command's argument, at most 128 bytes."`
	Category    *string `json:"category,omitempty" schema:"nullable,maxLength=128,text=line" doc:"Single-line, at most 128 bytes."`
}

// CanonicalIDResult is a registered contribution's canonical ID.
type CanonicalIDResult struct {
	ID string `json:"id" schema:"def=CanonicalID"`
}

// LocalIDParams names one of the plugin's commands, subagents, or footer items.
type LocalIDParams struct {
	ID string `json:"id" schema:"def=LocalID"`
}

// ToolRegisterParams registers a tool the model may call.
type ToolRegisterParams struct {
	ID               string          `json:"id" schema:"def=ToolID"`
	Label            *string         `json:"label,omitempty" schema:"nullable,maxLength=512,text=line" doc:"Single-line display label, at most 512 bytes. Defaults to the ID."`
	Description      string          `json:"description" schema:"minLength=1,maxLength=8192,text=multiline" doc:"Model-facing description, at most 8 KiB."`
	InputSchema      ToolInputSchema `json:"inputSchema"`
	ExecutionMode    *ExecutionMode  `json:"executionMode,omitempty" doc:"Defaults to sequential."`
	PromptSnippet    *string         `json:"promptSnippet,omitempty" schema:"nullable,maxLength=8192,text=multiline" doc:"At most 8 KiB."`
	PromptGuidelines []string        `json:"promptGuidelines,omitempty" schema:"maxItems=32,itemMaxLength=1024,itemText=multiline" doc:"Each at most 1 KiB."`
}

// ExecutionMode says whether a tool's calls may run alongside other calls in
// the same model response.
type ExecutionMode string

// Execution modes.
const (
	ExecutionSequential ExecutionMode = "sequential"
	ExecutionParallel   ExecutionMode = "parallel"
)

// EnumValues lists the execution modes.
func (ExecutionMode) EnumValues() []string {
	return []string{string(ExecutionSequential), string(ExecutionParallel)}
}

// ToolRegisterResult is a registered tool's canonical ID and model-facing name.
type ToolRegisterResult struct {
	ID        string `json:"id" schema:"def=CanonicalID"`
	ModelName string `json:"modelName" schema:"def=ModelToolName"`
}

// ToolIDParams names one of the plugin's tools.
type ToolIDParams struct {
	ID string `json:"id" schema:"def=ToolID"`
}

// ToolInputSchema is a tool's input schema in Kit's subset of JSON Schema
// draft 2020-12. The root is an object schema.
type ToolInputSchema json.RawMessage

// MarshalJSON emits the schema verbatim.
func (schema ToolInputSchema) MarshalJSON() ([]byte, error) { return json.RawMessage(schema), nil }

// UnmarshalJSON retains the schema verbatim.
func (schema *ToolInputSchema) UnmarshalJSON(data []byte) error {
	*schema = append((*schema)[:0], data...)
	return nil
}

// toolSchemaKeywords are the keywords a node of a tool input schema may use.
func toolSchemaKeywords() map[string]any {
	node := map[string]any{"$ref": "#/$defs/ToolSchemaNode"}
	number := map[string]any{"type": "number"}
	count := map[string]any{"type": "integer", "minimum": 0}
	return map[string]any{
		"$schema":              map[string]any{"const": "https://json-schema.org/draft/2020-12/schema"},
		"type":                 map[string]any{"enum": []string{"object", "array", "string", "number", "integer", "boolean", "null"}},
		"description":          map[string]any{"type": "string"},
		"properties":           map[string]any{"type": "object", "additionalProperties": node},
		"required":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"additionalProperties": map[string]any{"type": "boolean"},
		"items":                node,
		"enum":                 map[string]any{"type": "array"},
		"const":                map[string]any{},
		"anyOf":                map[string]any{"type": "array", "items": node},
		"minimum":              number,
		"maximum":              number,
		"exclusiveMinimum":     number,
		"exclusiveMaximum":     number,
		"minLength":            count,
		"maxLength":            count,
		"pattern":              map[string]any{"type": "string"},
		"minItems":             count,
		"maxItems":             count,
	}
}

// JSONSchema describes a tool input schema: a node whose type is object.
func (ToolInputSchema) JSONSchema() map[string]any {
	keywords := toolSchemaKeywords()
	keywords["type"] = map[string]any{"const": "object"}
	return map[string]any{"type": "object", "required": []string{"type"}, "properties": keywords, "additionalProperties": false,
		"description": "Kit's subset of JSON Schema draft 2020-12. Schemas cannot reference other schemas. At most 1,024 nodes and 16 levels."}
}

// ToolSchemaNode is one schema inside a tool input schema.
type ToolSchemaNode struct{}

// JSONSchema describes a tool input schema node.
func (ToolSchemaNode) JSONSchema() map[string]any {
	return map[string]any{"type": "object", "properties": toolSchemaKeywords(), "additionalProperties": false}
}

// SubagentRegisterParams registers a subagent definition Kit runs.
type SubagentRegisterParams struct {
	ID           string  `json:"id" schema:"def=LocalID"`
	Description  string  `json:"description" schema:"minLength=1,maxLength=1024,text=line-nonblank" doc:"Single-line, at most 1,024 bytes."`
	Instructions string  `json:"instructions" schema:"minLength=1,maxLength=131072,text=multiline-nonblank" doc:"The subagent's instructions, at most 128 KiB."`
	Model        *string `json:"model,omitempty" schema:"nullable,maxLength=256,text=line" doc:"Kit model selector; defaults to the session's model."`
}

// FooterSetParams creates or replaces one of the plugin's footer items.
type FooterSetParams struct {
	ID      string          `json:"id" schema:"def=LocalID"`
	Content []FooterSegment `json:"content" schema:"minItems=1,maxItems=32"`
}

// FooterSegment is one run of footer text, at most 4 KiB per item.
type FooterSegment struct {
	Text  string       `json:"text" schema:"maxLength=4096,text=line" doc:"Single-line. An item's segments hold at most 4 KiB of text in total."`
	Style *FooterStyle `json:"style,omitempty"`
}

// FooterStyle styles a footer segment with theme tokens, never colors.
type FooterStyle struct {
	FG            *ThemeToken `json:"fg,omitempty"`
	BG            *ThemeToken `json:"bg,omitempty"`
	Bold          *bool       `json:"bold,omitempty"`
	Dim           *bool       `json:"dim,omitempty"`
	Italic        *bool       `json:"italic,omitempty"`
	Underline     *bool       `json:"underline,omitempty"`
	Strikethrough *bool       `json:"strikethrough,omitempty"`
}

// FooterTargetParams names a footer item to hide or show.
type FooterTargetParams struct {
	ID string `json:"id" schema:"def=FooterTarget"`
}

// ThemeToken is a semantic color a client resolves with its theme.
type ThemeToken string

// ThemeTokens lists the theme tokens plugins may use.
func ThemeTokens() []string {
	return []string{
		"bg", "bgSurface", "bgMuted", "bgAccent", "bgTransparent",
		"borderDefault", "borderFocused", "borderAccent", "borderDebug", "borderStatus",
		"composerBashBorder", "composerBashExcludedBorder",
		"textPrimary", "textSecondary", "textMuted", "textPlaceholder", "textDebug",
		"userText", "userTextFocused", "userBorder", "assistantText", "toolText", "reviewText",
		"errorText", "warningText", "subagentText", "debugLabel", "metaText", "attachmentText", "cursor",
		"pickerBg", "pickerBorder", "pickerFocusedBg", "pickerFocusedText", "pickerItemText",
		"pickerScrollThumb", "pickerScrollTrack", "scrollbarFg", "scrollbarBg", "panelText",
		"progressNormal", "progressWarning", "progressCritical", "toggleOn",
		"diffAddedBg", "diffRemovedBg", "diffAddedContentBg", "diffRemovedContentBg",
		"diffAddedLineNumberBg", "diffRemovedLineNumberBg", "diffCursorBg", "diffCursorGutterBg",
		"diffCursorAddedBg", "diffCursorRemovedBg",
	}
}

// EnumValues lists the theme tokens.
func (ThemeToken) EnumValues() []string { return ThemeTokens() }
