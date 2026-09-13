package protocol

import (
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/attachmentmeta"
	"github.com/akonwi/kit/internal/identifier"
)

const (
	MaxAttachmentsPerPrompt  = 8
	MaxPromptAttachmentBytes = 20 << 20
	MaxImageAttachmentBytes  = 10 << 20
	MaxTextAttachmentBytes   = 1 << 20
)

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
