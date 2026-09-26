// Package modelimage owns the validation limits shared by model-visible image inputs.
package modelimage

import "github.com/akonwi/kit/internal/attachment"

const (
	// MaxBytes is the maximum encoded size of a model-visible image.
	MaxBytes = 10 << 20
	// MaxWidth is the maximum decoded width of a model-visible image.
	MaxWidth = 8192
	// MaxHeight is the maximum decoded height of a model-visible image.
	MaxHeight = 8192
	// MaxPixels is the maximum decoded pixel count of a model-visible image.
	MaxPixels = 12_000_000
)

// Limits returns the validation limits shared by image attachments and image inspection.
func Limits() attachment.ImageLimits {
	return attachment.ImageLimits{
		MaxBytes: MaxBytes,
		MaxWidth: MaxWidth, MaxHeight: MaxHeight, MaxPixels: MaxPixels,
	}
}
