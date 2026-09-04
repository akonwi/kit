package droids

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/url"
	"strings"
)

// message.go — the neutral conversation vocabulary shared by every layer.
// Providers translate to/from their wire format at the edge; the loop and
// storage only ever see these types.

// Role identifies who produced a message.
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "toolResult"
)

// Content is a sealed interface over the block types a message can carry.
type Content interface{ isContent() }

// TextContent is plain text emitted by a user or assistant.
type TextContent struct {
	Text string
	// Signature is opaque provider metadata (e.g. OpenAI responses item id)
	// that must be replayed on subsequent turns. Empty when unused.
	Signature string
}

func (TextContent) isContent() {}

// ThinkingContent is reasoning/thinking output from a model.
type ThinkingContent struct {
	Thinking  string
	Signature string
	// Redacted marks thinking hidden by safety filters; Signature then holds
	// the opaque encrypted payload for multi-turn continuity.
	Redacted bool
}

func (ThinkingContent) isContent() {}

// ImageContent is an unnamed visual block sourced from a fully qualified HTTPS
// URL or data URL.
type ImageContent struct {
	MediaType string // e.g. "image/png"
	URL       string
}

func (ImageContent) isContent() {}

// NewImageURL constructs an image block from a fully qualified HTTPS or data
// URL. URLs containing user credentials are rejected.
func NewImageURL(mediaType, rawURL string) (ImageContent, error) {
	if err := validateImageMediaType(mediaType); err != nil {
		return ImageContent{}, fmt.Errorf("droids: invalid image media type: %w", err)
	}
	if err := validateContentSource(rawURL, mediaType); err != nil {
		return ImageContent{}, fmt.Errorf("droids: invalid image URL: %w", err)
	}
	return ImageContent{MediaType: mediaType, URL: rawURL}, nil
}

// NewImageData constructs an image block containing base64-encoded inline data.
func NewImageData(mediaType string, data []byte) ImageContent {
	return ImageContent{MediaType: mediaType, URL: dataURL(mediaType, data)}
}

// FileContent is a named user-provided file sourced from a fully qualified
// HTTPS URL or data URL. Applications resolve durable attachment references
// into one of these provider-facing source forms before constructing messages.
type FileContent struct {
	Filename  string
	MediaType string
	URL       string
}

func (FileContent) isContent() {}

// NewFileURL constructs a named file block from a fully qualified HTTPS or data
// URL. URLs containing user credentials are rejected.
func NewFileURL(filename, mediaType, rawURL string) (FileContent, error) {
	if err := validateFileMetadata(filename, mediaType); err != nil {
		return FileContent{}, fmt.Errorf("droids: invalid file metadata: %w", err)
	}
	if err := validateContentSource(rawURL, mediaType); err != nil {
		return FileContent{}, fmt.Errorf("droids: invalid file URL: %w", err)
	}
	return FileContent{Filename: filename, MediaType: mediaType, URL: rawURL}, nil
}

// NewFileData constructs a named file block containing base64-encoded inline
// data.
func NewFileData(filename, mediaType string, data []byte) FileContent {
	return FileContent{Filename: filename, MediaType: mediaType, URL: dataURL(mediaType, data)}
}

func dataURL(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func validateContentSource(rawURL, mediaType string) error {
	if err := validateContentURL(rawURL); err != nil {
		return err
	}
	parsed, _ := url.Parse(rawURL)
	if strings.EqualFold(parsed.Scheme, "data") {
		return validateDataURL(parsed.Opaque, mediaType)
	}
	return nil
}

func validateDataURL(opaque, expectedMediaType string) error {
	header, payload, ok := strings.Cut(opaque, ",")
	if !ok || !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return fmt.Errorf("data URL must contain base64-encoded data")
	}
	header = header[:len(header)-len(";base64")]
	embeddedType, _, err := mime.ParseMediaType(header)
	if err != nil || embeddedType == "" {
		return fmt.Errorf("data URL has an invalid media type")
	}
	expectedType, _, err := mime.ParseMediaType(expectedMediaType)
	if err != nil || expectedType == "" {
		return fmt.Errorf("declared media type %q is invalid", expectedMediaType)
	}
	if !strings.EqualFold(embeddedType, expectedType) {
		return fmt.Errorf("data URL media type %q does not match declared media type %q", embeddedType, expectedType)
	}
	if payload == "" {
		return fmt.Errorf("data URL payload is empty")
	}
	decoder := base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(payload))
	if _, err := io.Copy(io.Discard, decoder); err != nil {
		return fmt.Errorf("data URL payload is not valid base64: %w", err)
	}
	return nil
}

func validateFileMetadata(filename, mediaType string) error {
	if strings.TrimSpace(filename) == "" {
		return fmt.Errorf("filename is required")
	}
	_, err := validateMediaType(mediaType)
	return err
}

func validateImageMediaType(mediaType string) error {
	parsed, err := validateMediaType(mediaType)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(strings.ToLower(parsed), "image/") {
		return fmt.Errorf("media type %q is not an image", mediaType)
	}
	return nil
}

func validateMediaType(mediaType string) (string, error) {
	if strings.TrimSpace(mediaType) == "" {
		return "", fmt.Errorf("media type is required")
	}
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil || parsed == "" {
		return "", fmt.Errorf("media type %q is invalid", mediaType)
	}
	parts := strings.Split(parsed, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == "*" || parts[1] == "*" {
		return "", fmt.Errorf("media type %q must be a concrete type/subtype", mediaType)
	}
	return parsed, nil
}

func validateContentURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL is required")
	}
	if rawURL != strings.TrimSpace(rawURL) {
		return fmt.Errorf("URL must not contain surrounding whitespace")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.User != nil {
		return fmt.Errorf("URL user credentials are not allowed")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		if !parsed.IsAbs() || parsed.Host == "" {
			return fmt.Errorf("HTTPS URL must be absolute and include a host")
		}
	case "data":
		if !parsed.IsAbs() || parsed.Opaque == "" {
			return fmt.Errorf("data URL must be absolute and include data")
		}
	default:
		return fmt.Errorf("URL scheme must be https or data")
	}
	return nil
}

// ToolCall is a model request to invoke a tool.
type ToolCall struct {
	ID        string
	Name      string
	Arguments []byte // raw JSON arguments
	// Signature is opaque provider metadata (for example, a Responses API
	// function-call item id) needed to replay the call on subsequent turns.
	Signature string
}

func (ToolCall) isContent() {}

// Message is a sealed interface over the three conversation message kinds.
// It is the durable unit persisted by Storage.
type Message interface {
	isMessage() //nolint:unused // seals the interface
	Role() Role
}

// UserMessage is input from the user.
type UserMessage struct {
	Content   []Content
	Timestamp int64 // unix millis
}

func (UserMessage) isMessage() {}
func (UserMessage) Role() Role { return RoleUser }

// ErrorKind classifies a provider failure without exposing provider-specific
// response text to callers that need stable recovery behavior.
type ErrorKind string

const (
	ErrorAuthentication ErrorKind = "authentication"
	ErrorEntitlement    ErrorKind = "entitlement"
	ErrorUsageLimit     ErrorKind = "usage_limit"
	ErrorRateLimit      ErrorKind = "rate_limit"
	ErrorTransport      ErrorKind = "transport"
	ErrorProtocol       ErrorKind = "protocol"
)

// AssistantMessage is a full model response for one turn.
type AssistantMessage struct {
	// ID is assigned by Droid before a provider attempt so the live message
	// lifecycle and durable projection share one identity. Providers ignore it;
	// manually constructed messages may leave it empty.
	ID            string
	Content       []Content // TextContent | ThinkingContent | ToolCall
	Provider      string
	Model         string
	ResponseModel string // concrete model when it differs from the requested one
	ResponseID    string
	// ProviderScope is an opaque credential-realm binding used to prevent
	// provider-specific replay under a different account.
	ProviderScope string
	Usage         Usage
	StopReason    StopReason
	ErrorKind     ErrorKind
	ErrorMessage  string
	Timestamp     int64
}

func (AssistantMessage) isMessage() {}
func (AssistantMessage) Role() Role { return RoleAssistant }

// Text returns the concatenated text content of the message (thinking and tool
// calls excluded), with blocks joined by newlines.
func (m AssistantMessage) Text() string {
	var b []byte
	for _, c := range m.Content {
		if t, ok := c.(TextContent); ok {
			if len(b) > 0 {
				b = append(b, '\n')
			}
			b = append(b, t.Text...)
		}
	}
	return string(b)
}

// ToolCalls returns the tool-call blocks in this message, in order.
func (m AssistantMessage) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, c := range m.Content {
		if tc, ok := c.(ToolCall); ok {
			calls = append(calls, tc)
		}
	}
	return calls
}

// ToolResultMessage is the result of executing a single tool call.
type ToolResultMessage struct {
	ToolCallID string
	ToolName   string
	Content    []Content // TextContent | ImageContent | FileContent
	Details    any
	IsError    bool
	Timestamp  int64
}

func (ToolResultMessage) isMessage() {}
func (ToolResultMessage) Role() Role { return RoleToolResult }

// StopReason explains why an assistant turn ended.
type StopReason string

const (
	StopReasonStop          StopReason = "stop"
	StopReasonLength        StopReason = "length"
	StopReasonToolUse       StopReason = "toolUse"
	StopReasonContextWindow StopReason = "contextWindow"
	StopReasonError         StopReason = "error"
	StopReasonAborted       StopReason = "aborted"
)
