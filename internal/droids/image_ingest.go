package droids

import (
	"errors"
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/droids/internal/imageprep"
)

// Provider-neutral limits for images entering canonical history (ADR 0037).
// They bound storage and decode cost; they are not provider limits, which
// image policies apply per request.
const (
	// MaxImageBytes bounds an image's raw encoded size.
	MaxImageBytes = 10 << 20
	// MaxImageWidth bounds an image's stored width in pixels.
	MaxImageWidth = 8192
	// MaxImageHeight bounds an image's stored height in pixels.
	MaxImageHeight = 8192
	// MaxImagePixels bounds an image's stored pixel count.
	MaxImagePixels = 12_000_000
)

// ingestToolResultImages applies the provider-neutral image limits to the
// inline images in a tool result before it enters canonical history. An image
// within the limits is kept and labeled with its detected media type; any
// other image is replaced by a text placeholder stating why. Remote images and
// non-image files are left unchanged.
func ingestToolResultImages(content []ResultContent) []ResultContent {
	var out []ResultContent
	for index, block := range content {
		file, ok := block.(FileContent)
		if !ok || !isImageMediaType(file.MediaType) || !isDataURL(file.URL) {
			if out != nil {
				out = append(out, block)
			}
			continue
		}
		ingested := ingestImage(file)
		if out == nil {
			if ingested == block {
				continue
			}
			out = append(make([]ResultContent, 0, len(content)), content[:index]...)
		}
		out = append(out, ingested)
	}
	if out == nil {
		return content
	}
	return out
}

// ingestImage returns file relabeled with its detected media type, or a
// placeholder when it exceeds the provider-neutral limits.
func ingestImage(file FileContent) ResultContent {
	data, err := decodeImageDataURL(file.URL)
	if err != nil || len(data) == 0 {
		return omittedIngestedImage(file, "the image data is invalid")
	}
	if len(data) > MaxImageBytes {
		return omittedIngestedImage(file, fmt.Sprintf("the image exceeds the %d MiB image size limit", MaxImageBytes>>20))
	}
	header, err := imageprep.Inspect(data)
	if err != nil {
		if errors.Is(err, imageprep.ErrUnsupportedFormat) {
			return omittedIngestedImage(file, "the image format is not supported")
		}
		return omittedIngestedImage(file, "the image could not be decoded")
	}
	if header.Width > MaxImageWidth || header.Height > MaxImageHeight ||
		int64(header.Width)*int64(header.Height) > MaxImagePixels {
		return omittedIngestedImage(file, fmt.Sprintf("the image's %d×%d px exceed the %d px, %d MP image limit",
			header.Width, header.Height, MaxImageWidth, MaxImagePixels/1_000_000))
	}
	if !strings.EqualFold(file.MediaType, header.MediaType) {
		file.MediaType, file.URL = header.MediaType, imageDataURL(header.MediaType, data)
	}
	return file
}

func omittedIngestedImage(file FileContent, reason string) TextContent {
	if file.Filename != "" {
		return TextContent{Text: fmt.Sprintf("[Image %q omitted: %s.]", file.Filename, reason)}
	}
	return TextContent{Text: fmt.Sprintf("[Image omitted: %s.]", reason)}
}

func isDataURL(rawURL string) bool {
	return len(rawURL) >= 5 && strings.EqualFold(rawURL[:5], "data:")
}
