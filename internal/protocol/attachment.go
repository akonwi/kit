package protocol

import (
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/attachmentmeta"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/modelimage"
)

const (
	MaxAttachmentsPerPrompt  = 8
	MaxPromptAttachmentBytes = 20 << 20
	MaxImageAttachmentBytes  = modelimage.MaxBytes
	MaxTextAttachmentBytes   = 1 << 20
	MaxAttachmentResolution  = 512
)

// AttachmentResolutionInput requests metadata for session-owned attachments.
type AttachmentResolutionInput struct {
	AttachmentIDs []string `json:"attachmentIds"`
}

// Validate checks the bounded canonical attachment identities.
func (input AttachmentResolutionInput) Validate() error {
	if len(input.AttachmentIDs) == 0 || len(input.AttachmentIDs) > MaxAttachmentResolution {
		return fmt.Errorf("attachment resolution count is invalid")
	}
	seen := make(map[string]struct{}, len(input.AttachmentIDs))
	for _, id := range input.AttachmentIDs {
		if !identifier.Valid(id, "attachment_") {
			return fmt.Errorf("attachment resolution id is invalid")
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("attachment resolution ids must be unique")
		}
		seen[id] = struct{}{}
	}
	return nil
}

// AttachmentResolution contains resolved metadata and unavailable identities.
type AttachmentResolution struct {
	Attachments          []AttachmentInfo `json:"attachments"`
	MissingAttachmentIDs []string         `json:"missingAttachmentIds,omitempty"`
}

// Validate checks identities, metadata, and the requested session boundary.
func (resolution AttachmentResolution) Validate(sessionID string, requested []string) error {
	positions := make(map[string]int, len(requested))
	for index, id := range requested {
		positions[id] = index
	}
	seen := make(map[string]struct{}, len(requested))
	lastPosition := -1
	for _, info := range resolution.Attachments {
		if err := info.Validate(); err != nil {
			return err
		}
		position, requestedID := positions[info.ID]
		if !requestedID || info.SessionID != sessionID || position <= lastPosition {
			return fmt.Errorf("resolved attachment identity is invalid")
		}
		if _, duplicate := seen[info.ID]; duplicate {
			return fmt.Errorf("resolved attachment identity is duplicated")
		}
		seen[info.ID], lastPosition = struct{}{}, position
	}
	for _, id := range resolution.MissingAttachmentIDs {
		if _, requestedID := positions[id]; !requestedID {
			return fmt.Errorf("missing attachment identity was not requested")
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("attachment resolution identity is duplicated")
		}
		seen[id] = struct{}{}
	}
	if len(seen) != len(requested) {
		return fmt.Errorf("attachment resolution is incomplete")
	}
	return nil
}

// AttachmentInfo is bounded metadata for server-owned attachment bytes.
type AttachmentInfo struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Filename  string `json:"filename"`
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	CreatedAt string `json:"createdAt"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

func (info AttachmentInfo) Validate() error {
	if !identifier.Valid(info.ID, "attachment_") {
		return fmt.Errorf("attachment id is invalid")
	}
	if !validProtocolText(info.SessionID, 256) || strings.TrimSpace(info.SessionID) != info.SessionID {
		return fmt.Errorf("attachment session id is invalid")
	}
	if !attachmentmeta.ValidFilename(info.Filename) {
		return fmt.Errorf("attachment filename is invalid")
	}
	mediaType := info.MediaType
	if !attachmentmeta.SupportedMediaType(mediaType) {
		return fmt.Errorf("attachment media type is invalid")
	}
	limit := int64(MaxTextAttachmentBytes)
	if strings.HasPrefix(mediaType, "image/") {
		limit = MaxImageAttachmentBytes
	}
	if info.Size <= 0 || info.Size > limit {
		return fmt.Errorf("attachment size is invalid")
	}
	if len(info.SHA256) != 64 {
		return fmt.Errorf("attachment checksum is invalid")
	}
	for _, character := range info.SHA256 {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return fmt.Errorf("attachment checksum is invalid")
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, info.CreatedAt); err != nil {
		return fmt.Errorf("attachment creation time is invalid")
	}
	if (info.Width == 0) != (info.Height == 0) || info.Width < 0 || info.Height < 0 || info.Width > 8192 || info.Height > 8192 || int64(info.Width)*int64(info.Height) > 12_000_000 {
		return fmt.Errorf("attachment dimensions are invalid")
	}
	if !strings.HasPrefix(mediaType, "image/") && (info.Width != 0 || info.Height != 0) {
		return fmt.Errorf("text attachment has image dimensions")
	}
	return nil
}
