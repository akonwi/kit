// Package modelimage applies the provider-neutral model-image limits, owned by
// Droids, to Kit's image attachment and image inspection inputs.
package modelimage

import (
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/attachment"
)

const (
	// MaxBytes is the maximum encoded size of a model-visible image.
	MaxBytes = droids.MaxImageBytes
	// MaxWidth is the maximum decoded width of a model-visible image.
	MaxWidth = droids.MaxImageWidth
	// MaxHeight is the maximum decoded height of a model-visible image.
	MaxHeight = droids.MaxImageHeight
	// MaxPixels is the maximum decoded pixel count of a model-visible image.
	MaxPixels = droids.MaxImagePixels
)

// Limits returns the validation limits shared by image attachments and image inspection.
func Limits() attachment.ImageLimits {
	return attachment.ImageLimits{
		MaxBytes: MaxBytes,
		MaxWidth: MaxWidth, MaxHeight: MaxHeight, MaxPixels: MaxPixels,
	}
}
