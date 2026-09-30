package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TranscriptContentKind identifies one renderer-neutral message content block.
type TranscriptContentKind string

const (
	TranscriptContentText        TranscriptContentKind = "text"
	TranscriptContentThinking    TranscriptContentKind = "thinking"
	TranscriptContentToolCall    TranscriptContentKind = "toolCall"
	TranscriptContentImage       TranscriptContentKind = "image"
	TranscriptContentFile        TranscriptContentKind = "file"
	TranscriptContentAnnotations TranscriptContentKind = "annotations"
)

// TranscriptContent is one ordered presentation content block of a message, a
// discriminated union by kind (ADR 0032). Payload is one of TextContent,
// ThinkingContent, ToolCallContent, ImageContent, FileContent, or
// AnnotationsContent.
type TranscriptContent struct {
	Payload TranscriptContentPayload `json:"-"`
}

// TranscriptContentPayload is the kind-specific data of one content block.
type TranscriptContentPayload interface {
	transcriptContentKind() TranscriptContentKind
}

// TextContent is visible message text.
type TextContent struct {
	Text string `json:"text"`
}

// ThinkingContent is assistant reasoning text.
type ThinkingContent struct {
	Text string `json:"text"`
}

// ToolCallContent is one assistant tool call with complete or explicitly
// truncated presentation arguments.
type ToolCallContent struct {
	ToolCallID         string `json:"toolCallId"`
	ToolName           string `json:"toolName"`
	Arguments          string `json:"arguments,omitempty"`
	ArgumentsTruncated bool   `json:"argumentsTruncated,omitempty"`
}

// ImageContent is an image, optionally backed by a stored attachment.
type ImageContent struct {
	Filename     string `json:"filename,omitempty"`
	MediaType    string `json:"mediaType"`
	AttachmentID string `json:"attachmentId,omitempty"`
}

// FileContent is a named file, optionally backed by a stored attachment.
type FileContent struct {
	Filename     string `json:"filename"`
	MediaType    string `json:"mediaType"`
	AttachmentID string `json:"attachmentId,omitempty"`
}

// AnnotationsContent is the annotation snapshot submitted with a prompt.
type AnnotationsContent struct {
	Annotations []SubmittedAnnotation `json:"annotations"`
}

func (TextContent) transcriptContentKind() TranscriptContentKind { return TranscriptContentText }
func (ThinkingContent) transcriptContentKind() TranscriptContentKind {
	return TranscriptContentThinking
}
func (ToolCallContent) transcriptContentKind() TranscriptContentKind {
	return TranscriptContentToolCall
}
func (ImageContent) transcriptContentKind() TranscriptContentKind { return TranscriptContentImage }
func (FileContent) transcriptContentKind() TranscriptContentKind  { return TranscriptContentFile }
func (AnnotationsContent) transcriptContentKind() TranscriptContentKind {
	return TranscriptContentAnnotations
}

// NewTranscriptContent wraps a payload as a content block.
func NewTranscriptContent(payload TranscriptContentPayload) TranscriptContent {
	return TranscriptContent{Payload: payload}
}

// TextBlock returns a text content block.
func TextBlock(text string) TranscriptContent {
	return TranscriptContent{Payload: TextContent{Text: text}}
}

// Kind returns the block's discriminator, or "" when it has no payload.
func (c TranscriptContent) Kind() TranscriptContentKind {
	if c.Payload == nil {
		return ""
	}
	variant, err := transcriptContentCodec.variant(c.Payload)
	if err != nil {
		return ""
	}
	return TranscriptContentKind(variant.kind)
}

// UnionVariants declares the kinds of TranscriptContent for contract generation.
func (TranscriptContent) UnionVariants() []UnionVariant {
	return append([]UnionVariant(nil), transcriptContentCodec.variants...)
}

// transcriptContentWire is the flat pre-union record layout of TranscriptContent.
type transcriptContentWire struct {
	Kind               TranscriptContentKind `json:"kind"`
	Text               string                `json:"text,omitempty"`
	ToolCallID         string                `json:"toolCallId,omitempty"`
	ToolName           string                `json:"toolName,omitempty"`
	Arguments          string                `json:"arguments,omitempty"`
	ArgumentsTruncated bool                  `json:"argumentsTruncated,omitempty"`
	Filename           string                `json:"filename,omitempty"`
	MediaType          string                `json:"mediaType,omitempty"`
	AttachmentID       string                `json:"attachmentId,omitempty"`
	Annotations        []SubmittedAnnotation `json:"annotations,omitempty"`
}

var transcriptContentCodec = newUnionCodec[TranscriptContent, transcriptContentWire]([]UnionVariant{
	{Kind: string(TranscriptContentText), Payload: TextContent{}},
	{Kind: string(TranscriptContentThinking), Payload: ThinkingContent{}},
	{Kind: string(TranscriptContentToolCall), Payload: ToolCallContent{}},
	{Kind: string(TranscriptContentImage), Payload: ImageContent{}},
	{Kind: string(TranscriptContentFile), Payload: FileContent{}},
	{Kind: string(TranscriptContentAnnotations), Payload: AnnotationsContent{}},
})

// MarshalJSON encodes the block as one flat object discriminated by kind.
func (c TranscriptContent) MarshalJSON() ([]byte, error) {
	return transcriptContentCodec.marshal(c, c.Payload)
}

// UnmarshalJSON strictly decodes one block, rejecting unknown kinds and
// members that the kind does not carry.
func (c *TranscriptContent) UnmarshalJSON(data []byte) error {
	var decoded TranscriptContent
	payload, err := transcriptContentCodec.unmarshal(data, &decoded)
	if err != nil {
		return err
	}
	decoded.Payload = payload.(TranscriptContentPayload)
	*c = decoded
	return nil
}

// ContextContent is content carried by external context: text, images, and files.
type ContextContent []TranscriptContent

// ToolResultContent is content produced by a tool: text, images, and files.
type ToolResultContent []TranscriptContent

// ContentKinds lists the kinds context content may carry.
func (ContextContent) ContentKinds() []TranscriptContentKind {
	return []TranscriptContentKind{TranscriptContentText, TranscriptContentImage, TranscriptContentFile}
}

// ContentKinds lists the kinds tool result content may carry.
func (ToolResultContent) ContentKinds() []TranscriptContentKind {
	return []TranscriptContentKind{TranscriptContentText, TranscriptContentImage, TranscriptContentFile}
}

// UnmarshalJSON decodes context content, rejecting kinds it cannot carry.
func (c *ContextContent) UnmarshalJSON(data []byte) error {
	return decodeContentSubset(data, (*[]TranscriptContent)(c), ContextContent{}.ContentKinds(), "context")
}

// UnmarshalJSON decodes tool result content, rejecting kinds it cannot carry.
func (c *ToolResultContent) UnmarshalJSON(data []byte) error {
	return decodeContentSubset(data, (*[]TranscriptContent)(c), ToolResultContent{}.ContentKinds(), "tool result")
}

func decodeContentSubset(data []byte, target *[]TranscriptContent, kinds []TranscriptContentKind, label string) error {
	var blocks []TranscriptContent
	if err := json.Unmarshal(data, &blocks); err != nil {
		return err
	}
	for index, block := range blocks {
		if !contentKindIn(block.Kind(), kinds) {
			return fmt.Errorf("%s content block %d kind %q is invalid", label, index, block.Kind())
		}
	}
	*target = blocks
	return nil
}

func transcriptContentSize(content TranscriptContent) int {
	switch block := content.Payload.(type) {
	case TextContent:
		return len(block.Text)
	case ThinkingContent:
		return len(block.Text)
	case ToolCallContent:
		return len(block.ToolCallID) + len(block.ToolName) + len(block.Arguments)
	case ImageContent:
		return len(block.Filename) + len(block.MediaType) + len(block.AttachmentID)
	case FileContent:
		return len(block.Filename) + len(block.MediaType) + len(block.AttachmentID)
	case AnnotationsContent:
		encoded, err := json.Marshal(block.Annotations)
		if err != nil {
			return MaxAnnotationsPerPrompt * MaxAnnotationBodyBytes
		}
		return len(encoded)
	}
	return 0
}

func contentKindIn(kind TranscriptContentKind, kinds []TranscriptContentKind) bool {
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

// validate checks one block's own invariants.
func (c TranscriptContent) validate() error {
	switch block := c.Payload.(type) {
	case TextContent:
		if block.Text == "" {
			return fmt.Errorf("text content requires text")
		}
	case ThinkingContent:
		if block.Text == "" {
			return fmt.Errorf("thinking content requires text")
		}
	case ToolCallContent:
		if block.ToolCallID == "" || block.ToolName == "" {
			return fmt.Errorf("tool call requires call id and name only")
		}
		if (block.Arguments == "") == !block.ArgumentsTruncated {
			return fmt.Errorf("tool call requires either complete or explicitly truncated arguments")
		}
	case ImageContent:
		if !validMediaType(block.MediaType, true) {
			return fmt.Errorf("image content requires an image media type without text or tool metadata")
		}
	case FileContent:
		if strings.TrimSpace(block.Filename) == "" || !validMediaType(block.MediaType, false) {
			return fmt.Errorf("file content requires filename and media type only")
		}
	case AnnotationsContent:
		if len(block.Annotations) == 0 || len(block.Annotations) > MaxAnnotationsPerPrompt {
			return fmt.Errorf("annotation content is invalid")
		}
		for _, annotation := range block.Annotations {
			if err := annotation.Validate(); err != nil {
				return err
			}
		}
	case nil:
		return fmt.Errorf("content block has no kind")
	default:
		return fmt.Errorf("kind %q is invalid", c.Kind())
	}
	return nil
}
