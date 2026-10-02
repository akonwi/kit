package droids

import (
	"fmt"
	"slices"
)

// image_policy.go — per-model image contracts (ADR 0037). Every provider
// declares an ImagePolicy for every model it serves; text-only models declare
// the zero policy.

// ImagePlacement identifies where an image appears in a request.
type ImagePlacement string

const (
	// ImagePlacementUser is an image in user input.
	ImagePlacementUser ImagePlacement = "user"
	// ImagePlacementContext is an image in an application context message.
	ImagePlacementContext ImagePlacement = "context"
	// ImagePlacementToolResult is an image in a tool result.
	ImagePlacementToolResult ImagePlacement = "tool_result"
)

// ImageSourceKind identifies how image bytes reach a provider.
type ImageSourceKind string

const (
	// ImageSourceData is an inline base64 data URL.
	ImageSourceData ImageSourceKind = "data"
	// ImageSourceHTTPS is a remote HTTPS URL fetched by the provider.
	ImageSourceHTTPS ImageSourceKind = "https"
)

// Image media types an ImagePolicy may accept. Kit produces only these.
const (
	ImageJPEG = "image/jpeg"
	ImagePNG  = "image/png"
	ImageGIF  = "image/gif"
	ImageWebP = "image/webp"
)

// ImageFit returns the dimensions a model processes for a w×h image.
type ImageFit func(w, h int) (int, int)

// ManyImageRule applies a stricter fit to every image in a request that
// carries more than Above images.
type ManyImageRule struct {
	Above int
	Fit   ImageFit
}

// ImagePolicy is a model's contract for image content. The zero value accepts
// no images.
type ImagePolicy struct {
	// Placements lists where images may appear. Empty means the model accepts
	// no images.
	Placements []ImagePlacement
	// Formats lists media types the provider accepts as sent. It must include
	// JPEG or PNG so non-conforming images can be prepared.
	Formats []string
	// Sources lists accepted source kinds. Kit produces inline images, so a
	// policy that accepts images must accept ImageSourceData.
	Sources []ImageSourceKind

	// Fit returns the dimensions the model processes for a w×h image under
	// the request options the provider sends. Nil keeps source dimensions.
	Fit ImageFit
	// MaxEncodedBytes bounds one image's base64-encoded size. Zero means the
	// provider documents no limit.
	MaxEncodedBytes int64
	// MaxImages bounds the images in one request. Zero means no
	// provider-specific bound beyond the runtime's retention window.
	MaxImages int
	// MaxRequestImageBytes bounds the total base64-encoded image bytes in one
	// request. Zero means no provider-specific bound.
	MaxRequestImageBytes int64
	// ManyImages, when set, applies a stricter fit to every image in a request
	// that carries more than ManyImages.Above images.
	ManyImages *ManyImageRule
}

// AcceptsImages reports whether the policy accepts images anywhere.
func (p ImagePolicy) AcceptsImages() bool { return len(p.Placements) > 0 }

// Accepts reports whether the policy accepts images at placement.
func (p ImagePolicy) Accepts(placement ImagePlacement) bool {
	return slices.Contains(p.Placements, placement)
}

// AcceptsFormat reports whether the policy accepts mediaType as sent.
func (p ImagePolicy) AcceptsFormat(mediaType string) bool {
	return slices.Contains(p.Formats, mediaType)
}

// AcceptsSource reports whether the policy accepts source.
func (p ImagePolicy) AcceptsSource(source ImageSourceKind) bool {
	return slices.Contains(p.Sources, source)
}

func (p ImagePolicy) clone() ImagePolicy {
	p.Placements = slices.Clone(p.Placements)
	p.Formats = slices.Clone(p.Formats)
	p.Sources = slices.Clone(p.Sources)
	if p.ManyImages != nil {
		rule := *p.ManyImages
		p.ManyImages = &rule
	}
	return p
}

// validate reports structural errors in the policy.
func (p ImagePolicy) validate() error {
	if !p.AcceptsImages() {
		if len(p.Formats) > 0 || len(p.Sources) > 0 || p.Fit != nil || p.ManyImages != nil ||
			p.MaxEncodedBytes != 0 || p.MaxImages != 0 || p.MaxRequestImageBytes != 0 {
			return fmt.Errorf("image constraints are declared without placements")
		}
		return nil
	}
	if err := validateImageSet("placement", p.Placements, []ImagePlacement{
		ImagePlacementUser, ImagePlacementContext, ImagePlacementToolResult,
	}); err != nil {
		return err
	}
	if err := validateImageSet("format", p.Formats, []string{ImageJPEG, ImagePNG, ImageGIF, ImageWebP}); err != nil {
		return err
	}
	if !p.AcceptsFormat(ImageJPEG) && !p.AcceptsFormat(ImagePNG) {
		return fmt.Errorf("formats must include %s or %s", ImageJPEG, ImagePNG)
	}
	if err := validateImageSet("source", p.Sources, []ImageSourceKind{ImageSourceData, ImageSourceHTTPS}); err != nil {
		return err
	}
	if !p.AcceptsSource(ImageSourceData) {
		return fmt.Errorf("sources must include %q", ImageSourceData)
	}
	if p.MaxEncodedBytes < 0 || p.MaxImages < 0 || p.MaxRequestImageBytes < 0 {
		return fmt.Errorf("image limits must not be negative")
	}
	if p.ManyImages != nil && (p.ManyImages.Above <= 0 || p.ManyImages.Fit == nil) {
		return fmt.Errorf("many-image rule requires a positive threshold and a fit")
	}
	return nil
}

func validateImageSet[T comparable](kind string, values, allowed []T) error {
	if len(values) == 0 {
		return fmt.Errorf("at least one %s is required", kind)
	}
	for index, value := range values {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("unsupported %s %v", kind, value)
		}
		if slices.Contains(values[:index], value) {
			return fmt.Errorf("duplicate %s %v", kind, value)
		}
	}
	return nil
}

// validateModelImagePolicy checks that policy is well formed and agrees with
// the model's advertised image input.
func validateModelImagePolicy(model Model, policy ImagePolicy) error {
	if err := policy.validate(); err != nil {
		return fmt.Errorf("droids: model %s/%s image policy: %w", model.Provider, model.ID, err)
	}
	advertises := containsString(model.Input, "image")
	switch {
	case advertises && !policy.AcceptsImages():
		return fmt.Errorf("droids: model %s/%s advertises image input but its image policy accepts no images", model.Provider, model.ID)
	case !advertises && policy.AcceptsImages():
		return fmt.Errorf("droids: model %s/%s accepts images in its image policy but does not advertise image input", model.Provider, model.ID)
	}
	return nil
}
