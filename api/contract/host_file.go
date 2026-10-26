package contract

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	// MaxHostFileImageBytes bounds the encoded image a host file read returns.
	MaxHostFileImageBytes = 16 << 20
	// MaxHostFileImagePixels bounds the decoded image a host file read returns.
	MaxHostFileImagePixels = 24_000_000
)

// ReadHostFileInput requests a bounded, view-only read of one file on the
// session server's machine. Unlike a workspace read it is not confined to the
// session's working directory.
type ReadHostFileInput struct {
	// Path is absolute, or "~" or "~/…" for the server user's home directory.
	Path string `json:"path"`
}

// HostFileKind identifies what a host file read returned.
type HostFileKind string

const (
	HostFileText  HostFileKind = "text"
	HostFileImage HostFileKind = "image"
)

// HostFileRead is one bounded observation of a file on the server's machine:
// a UTF-8 text preview or a complete supported raster image.
type HostFileRead struct {
	Payload HostFilePayload
}

// HostFilePayload is the sealed set of host file read kinds.
type HostFilePayload interface{ hostFileKind() HostFileKind }

// HostTextFile is a bounded UTF-8 preview of a host file. Size is the size
// the file reported, which some files, such as those under /proc, report as
// zero or a fixed block size whatever their content.
type HostTextFile struct {
	SessionID string `json:"sessionId"`
	// Path is the absolute, cleaned path that was read, with "~" expanded.
	Path             string `json:"path"`
	Size             int64  `json:"size,omitempty"`
	ModifiedAt       string `json:"modifiedAt,omitempty"`
	Encoding         string `json:"encoding"`
	Content          string `json:"content,omitempty"`
	ReturnedBytes    int    `json:"returnedBytes,omitempty"`
	ReturnedLines    int    `json:"returnedLines,omitempty"`
	Truncated        bool   `json:"truncated,omitempty"`
	TruncationReason string `json:"truncationReason,omitempty"`
}

// HostImageFile is a complete PNG, JPEG, GIF, or WebP host file.
type HostImageFile struct {
	SessionID  string `json:"sessionId"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
	MediaType  string `json:"mediaType"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Data       []byte `json:"data"`
}

func (HostTextFile) hostFileKind() HostFileKind  { return HostFileText }
func (HostImageFile) hostFileKind() HostFileKind { return HostFileImage }

// Kind returns the read's discriminator, or "" when it has no payload.
func (r HostFileRead) Kind() HostFileKind {
	if r.Payload == nil {
		return ""
	}
	return r.Payload.hostFileKind()
}

// Text returns the text preview, or nil for another kind.
func (r HostFileRead) Text() *HostTextFile {
	if text, ok := r.Payload.(HostTextFile); ok {
		return &text
	}
	return nil
}

// Image returns the image, or nil for another kind.
func (r HostFileRead) Image() *HostImageFile {
	if image, ok := r.Payload.(HostImageFile); ok {
		return &image
	}
	return nil
}

// SessionID returns the session the read belongs to.
func (r HostFileRead) SessionID() string {
	switch payload := r.Payload.(type) {
	case HostTextFile:
		return payload.SessionID
	case HostImageFile:
		return payload.SessionID
	}
	return ""
}

// Path returns the absolute path that was read.
func (r HostFileRead) Path() string {
	switch payload := r.Payload.(type) {
	case HostTextFile:
		return payload.Path
	case HostImageFile:
		return payload.Path
	}
	return ""
}

// UnionVariants declares the kinds of HostFileRead for contract generation.
func (HostFileRead) UnionVariants() []UnionVariant {
	return append([]UnionVariant(nil), hostFileReadCodec.variants...)
}

type hostFileReadWire struct {
	Kind             HostFileKind `json:"kind"`
	SessionID        string       `json:"sessionId"`
	Path             string       `json:"path"`
	Size             int64        `json:"size,omitempty"`
	ModifiedAt       string       `json:"modifiedAt,omitempty"`
	Encoding         string       `json:"encoding,omitempty"`
	Content          string       `json:"content,omitempty"`
	ReturnedBytes    int          `json:"returnedBytes,omitempty"`
	ReturnedLines    int          `json:"returnedLines,omitempty"`
	Truncated        bool         `json:"truncated,omitempty"`
	TruncationReason string       `json:"truncationReason,omitempty"`
	MediaType        string       `json:"mediaType,omitempty"`
	Width            int          `json:"width,omitempty"`
	Height           int          `json:"height,omitempty"`
	Data             []byte       `json:"data,omitempty"`
}

var hostFileReadCodec = newUnionCodec[HostFileRead, hostFileReadWire]([]UnionVariant{
	{Kind: string(HostFileText), Payload: HostTextFile{}},
	{Kind: string(HostFileImage), Payload: HostImageFile{}},
})

// MarshalJSON encodes the read as one flat object discriminated by kind.
func (r HostFileRead) MarshalJSON() ([]byte, error) {
	return hostFileReadCodec.marshal(r, r.Payload)
}

// UnmarshalJSON strictly decodes one read, rejecting unknown kinds and
// members the kind does not carry.
func (r *HostFileRead) UnmarshalJSON(data []byte) error {
	var decoded HostFileRead
	payload, err := hostFileReadCodec.unmarshal(data, &decoded)
	if err != nil {
		return err
	}
	decoded.Payload = payload.(HostFilePayload)
	*r = decoded
	return nil
}

// ValidateHostFilePath checks a requested host path: absolute, or "~" or
// "~/…" when home is allowed, renderer-safe, and within the path bound.
func ValidateHostFilePath(value string, allowHome bool) error {
	if len(value) > MaxWorkspacePathBytes {
		return fmt.Errorf("host file path is too long")
	}
	if !validPathText(value) {
		return fmt.Errorf("host file path is invalid")
	}
	if allowHome && (value == "~" || strings.HasPrefix(value, "~/")) {
		return nil
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("host file path must be absolute")
	}
	return nil
}

// Validate checks the request before it crosses the boundary.
func (in ReadHostFileInput) Validate() error {
	return ValidateHostFilePath(in.Path, true)
}

// Validate checks one host file read and its kind's invariants.
func (r HostFileRead) Validate() error {
	switch payload := r.Payload.(type) {
	case HostTextFile:
		return payload.Validate()
	case HostImageFile:
		return payload.Validate()
	default:
		return fmt.Errorf("host file read has no kind")
	}
}

func validHostFileIdentity(sessionID, value string, size int64, modifiedAt string) bool {
	return sessionID != "" && ValidateHostFilePath(value, false) == nil && filepath.Clean(value) == value &&
		size >= 0 && (modifiedAt == "" || parseWorkspaceTime(modifiedAt) == nil)
}

// Validate checks a text preview's identity, encoding, and bounds.
func (t HostTextFile) Validate() error {
	if !validHostFileIdentity(t.SessionID, t.Path, t.Size, t.ModifiedAt) || t.Encoding != "utf-8" ||
		!utf8.ValidString(t.Content) || strings.IndexByte(t.Content, 0) >= 0 || len(t.Content) != t.ReturnedBytes ||
		workspaceLineCount(t.Content) != t.ReturnedLines || t.ReturnedBytes > MaxWorkspacePreviewBytes ||
		t.ReturnedLines > MaxWorkspacePreviewLines {
		return fmt.Errorf("host text file is invalid")
	}
	if t.Truncated != (t.TruncationReason != "") || (t.TruncationReason != "" && t.TruncationReason != "byte_limit" && t.TruncationReason != "line_limit") {
		return fmt.Errorf("host text file truncation is invalid")
	}
	return nil
}

// Validate checks an image's identity, type, and bounds.
func (i HostImageFile) Validate() error {
	switch i.MediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return fmt.Errorf("host image media type is unsupported")
	}
	if !validHostFileIdentity(i.SessionID, i.Path, i.Size, i.ModifiedAt) || len(i.Data) == 0 ||
		int64(len(i.Data)) != i.Size || len(i.Data) > MaxHostFileImageBytes || i.Width <= 0 || i.Height <= 0 ||
		int64(i.Width)*int64(i.Height) > MaxHostFileImagePixels {
		return fmt.Errorf("host image file is invalid")
	}
	return nil
}
