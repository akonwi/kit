package pluginprotocol

import "encoding/json"

// ToastParams shows a notification in the session's clients.
type ToastParams struct {
	Title      string       `json:"title" schema:"minLength=1,maxLength=1024,text=line-nonblank" doc:"Single-line, at most 1,024 bytes."`
	Subtitle   *string      `json:"subtitle,omitempty" schema:"nullable,maxLength=4096,text=multiline" doc:"At most 4,096 bytes; may contain newlines."`
	Variant    ToastVariant `json:"variant"`
	Persistent *bool        `json:"persistent,omitempty" doc:"Keep the notification until the user dismisses it."`
}

// ToastVariant is a notification's severity.
type ToastVariant string

// Toast variants.
const (
	ToastInfo    ToastVariant = "info"
	ToastWarning ToastVariant = "warning"
	ToastError   ToastVariant = "error"
)

// EnumValues lists the toast variants.
func (ToastVariant) EnumValues() []string {
	return []string{string(ToastInfo), string(ToastWarning), string(ToastError)}
}

// ConfirmParams asks the user a yes-or-no question.
type ConfirmParams struct {
	Title        string     `json:"title" schema:"minLength=1,maxLength=512,text=line-nonblank" doc:"Single-line, at most 512 bytes."`
	Message      *string    `json:"message,omitempty" schema:"nullable,maxLength=8192,text=multiline" doc:"Markdown, at most 8 KiB."`
	ConfirmLabel *string    `json:"confirmLabel,omitempty" schema:"nullable,maxLength=128,text=line" doc:"At most 128 bytes."`
	CancelLabel  *string    `json:"cancelLabel,omitempty" schema:"nullable,maxLength=128,text=line" doc:"At most 128 bytes."`
	DefaultValue *bool      `json:"defaultValue,omitempty" doc:"The answer that has focus initially."`
	ParentID     *RequestID `json:"parentId,omitempty" doc:"A pending Kit request this dialog serves. Kit cancels the dialog when that request ends."`
}

// InputParams asks the user for text.
type InputParams struct {
	Title        string     `json:"title" schema:"minLength=1,maxLength=512,text=line-nonblank" doc:"Single-line, at most 512 bytes."`
	Message      *string    `json:"message,omitempty" schema:"nullable,maxLength=8192,text=multiline" doc:"Markdown, at most 8 KiB."`
	Placeholder  *string    `json:"placeholder,omitempty" schema:"nullable,maxLength=512,text=line" doc:"Single-line, at most 512 bytes."`
	InitialValue *string    `json:"initialValue,omitempty" schema:"nullable,maxLength=16384,text=multiline" doc:"At most 16 KiB."`
	ParentID     *RequestID `json:"parentId,omitempty" doc:"A pending Kit request this dialog serves. Kit cancels the dialog when that request ends."`
}

// SelectParams asks the user to choose one option.
type SelectParams struct {
	Title       string         `json:"title" schema:"minLength=1,maxLength=512,text=line-nonblank" doc:"Single-line, at most 512 bytes."`
	Message     *string        `json:"message,omitempty" schema:"nullable,maxLength=8192,text=multiline" doc:"Markdown, at most 8 KiB."`
	Options     []SelectOption `json:"options" schema:"minItems=1,maxItems=64"`
	Placeholder *string        `json:"placeholder,omitempty" schema:"nullable,maxLength=512,text=line" doc:"Single-line, at most 512 bytes."`
	ParentID    *RequestID     `json:"parentId,omitempty" doc:"A pending Kit request this dialog serves. Kit cancels the dialog when that request ends."`
}

// SelectOption is one choice. Its value is returned verbatim when chosen.
type SelectOption struct {
	Label       string          `json:"label" schema:"minLength=1,maxLength=512,text=line-nonblank" doc:"Single-line, at most 512 bytes."`
	Value       json.RawMessage `json:"value" schema:"required" doc:"Any JSON, at most 16 KiB; null is a valid value."`
	Description *string         `json:"description,omitempty" schema:"nullable,maxLength=8192,text=multiline" doc:"At most 8 KiB."`
}

// ConfirmResult is the user's answer: false when they decline or cancel.
type ConfirmResult bool

// InputResult is the entered text, or null when the user cancels. An empty
// string is an answer, not a cancellation.
type InputResult string

// JSONSchema describes text or null.
func (InputResult) JSONSchema() map[string]any {
	return map[string]any{"type": []string{"string", "null"}}
}

// SelectResult is the chosen option's value, or null when the user cancels.
type SelectResult struct {
	Value json.RawMessage `json:"value"`
}

// JSONSchema describes the chosen value, or null.
func (SelectResult) JSONSchema() map[string]any {
	return map[string]any{"oneOf": []any{
		map[string]any{"type": "object", "required": []string{"value"}, "properties": map[string]any{"value": map[string]any{}}},
		map[string]any{"type": "null"},
	}}
}

// SubmitMessageParams starts a turn in the plugin's idle session with a
// message attributed to the plugin.
type SubmitMessageParams struct {
	Text           string  `json:"text" schema:"minLength=1,maxLength=131072,text=message" doc:"At most 128 KiB; must not be blank or contain NUL."`
	IdempotencyKey *string `json:"idempotencyKey,omitempty" schema:"nullable,def=IdempotencyKey" doc:"Repeating a key with the same text returns the original result."`
}

// SubmitMessageResult identifies the admitted message and the turn it started.
type SubmitMessageResult struct {
	MessageID string `json:"messageId" schema:"minLength=1"`
	TurnID    string `json:"turnId" schema:"minLength=1"`
}

// OpenURLParams asks the session's attached clients to open a web page.
type OpenURLParams struct {
	URL string `json:"url" schema:"minLength=1,maxLength=2048,url" doc:"An absolute http or https URL without credentials."`
}
