// Package attachmentbridge adapts attachment downloads and image decoding
// that Ard cannot express: a three-result download and image scaling.
package attachmentbridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// maxSourcePixels bounds a decoded image so a small file cannot expand into
// an unbounded allocation.
const maxSourcePixels = 64 << 20

// Download is an attachment's metadata and content. The caller closes
// Content.
type Download struct {
	Info    protocol.AttachmentInfo
	Content io.ReadCloser
}

// Open starts downloading an attachment of the session.
func Open(session *kit.Session, ctx context.Context, attachmentID string) (Download, error) {
	info, content, err := session.OpenAttachment(ctx, attachmentID)
	if err != nil {
		return Download{}, err
	}
	return Download{Info: info, Content: content}, nil
}

// Pixels is a scaled image as straight-alpha RGBA8 bytes in row-major order.
type Pixels struct {
	Width  int
	Height int
	RGBA   []byte
}

// Decode reads a PNG, JPEG, GIF, or WebP image and scales it down, keeping
// its proportions, to fit within maxWidth by maxHeight pixels. Smaller images
// keep their size.
func Decode(content io.Reader, maxWidth, maxHeight int) (Pixels, error) {
	if maxWidth <= 0 || maxHeight <= 0 {
		return Pixels{}, errors.New("preview bounds must be positive")
	}
	encoded, err := io.ReadAll(io.LimitReader(content, protocol.MaxImageAttachmentBytes+1))
	if err != nil {
		return Pixels{}, err
	}
	if len(encoded) > protocol.MaxImageAttachmentBytes {
		return Pixels{}, errors.New("image is too large to preview")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		return Pixels{}, fmt.Errorf("unsupported image: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > maxSourcePixels {
		return Pixels{}, errors.New("image dimensions are too large to preview")
	}
	source, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		return Pixels{}, fmt.Errorf("unsupported image: %w", err)
	}
	bounds := source.Bounds()
	width, height := fit(bounds.Dx(), bounds.Dy(), maxWidth, maxHeight)
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	if width == bounds.Dx() && height == bounds.Dy() {
		draw.Copy(target, image.Point{}, source, bounds, draw.Src, nil)
	} else {
		// CatmullRom averages the source region behind each output pixel, so
		// shrunken screenshots stay legible.
		draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Src, nil)
	}
	return Pixels{Width: width, Height: height, RGBA: target.Pix}, nil
}

// fit scales width by height down to fit within the bounds, keeping the
// proportions and at least one pixel on each side.
func fit(width, height, maxWidth, maxHeight int) (int, int) {
	if width <= maxWidth && height <= maxHeight {
		return width, height
	}
	if width*maxHeight > height*maxWidth {
		return maxWidth, max(1, height*maxWidth/width)
	}
	return max(1, width*maxHeight/height), maxHeight
}
