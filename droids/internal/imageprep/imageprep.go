// Package imageprep prepares raster images for model requests. It decodes
// JPEG, PNG, GIF, and WebP sources, applies EXIF orientation, downscales to
// caller-selected dimensions, and re-encodes deterministically into an
// accepted format within an encoded-size limit. Conforming sources keep their
// original bytes.
//
// The package is pure Go and holds no provider knowledge: callers express
// provider constraints as a Target.
package imageprep

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // registers GIF decoding
	"image/jpeg"
	"image/png"
	"slices"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers WebP decoding
)

// Media types understood by the package.
const (
	JPEG = "image/jpeg"
	PNG  = "image/png"
	GIF  = "image/gif"
	WebP = "image/webp"
)

// MaxSourcePixels bounds the decoded size of a source image. It limits memory
// use during preparation; it is not a provider limit.
const MaxSourcePixels = 24_000_000

// jpegQuality is the fixed quality for JPEG output. A fixed value keeps output
// deterministic and avoids heavy compression artifacts on text.
const jpegQuality = 85

var (
	// ErrUnsupportedFormat reports a source that is not JPEG, PNG, GIF, or
	// WebP, or a target that accepts no format the package can produce.
	ErrUnsupportedFormat = errors.New("unsupported image format")
	// ErrUndecodable reports a source that cannot be decoded.
	ErrUndecodable = errors.New("image could not be decoded")
	// ErrSourceTooLarge reports a source whose dimensions exceed
	// MaxSourcePixels.
	ErrSourceTooLarge = errors.New("image dimensions exceed the processing limit")
	// ErrEncodedLimit reports that no accepted encoding fits the target's
	// encoded-size limit.
	ErrEncodedLimit = errors.New("image exceeds the encoded size limit")
)

// Target describes what a prepared image must satisfy.
type Target struct {
	// Formats lists accepted media types. An accepted, conforming source is
	// sent unchanged; otherwise output is encoded as JPEG or PNG, so a target
	// must accept at least one of them to prepare non-conforming images.
	Formats []string
	// Fit returns the dimensions to prepare for an oriented w×h source. A nil
	// Fit keeps source dimensions. Results larger than the source are clamped:
	// images are never enlarged.
	Fit func(w, h int) (int, int)
	// MaxEncodedBytes bounds the base64-encoded size of the output. Zero means
	// unlimited.
	MaxEncodedBytes int64
}

// Result is a prepared image.
type Result struct {
	// MediaType and Data are the image to send.
	MediaType string
	Data      []byte
	// Width and Height are the dimensions of Data.
	Width, Height int
	// SourceWidth and SourceHeight are the source dimensions after
	// orientation.
	SourceWidth, SourceHeight int
	// Changed reports whether Data differs from the source bytes.
	Changed bool
}

// Resized reports whether the prepared dimensions differ from the source.
func (r Result) Resized() bool {
	return r.Width != r.SourceWidth || r.Height != r.SourceHeight
}

// Preparer prepares images and caches prepared output. It is safe for
// concurrent use. Results share their Data with the cache and with the source
// passed to Prepare; callers must not modify it.
type Preparer struct {
	cache    *lru
	verified *keySet
}

// verifiedSources bounds how many conforming sources a Preparer remembers as
// fully decodable.
const verifiedSources = 4096

// New returns a Preparer whose cache retains at most cacheBytes of prepared
// image data. A non-positive capacity disables caching of prepared output;
// sources sent unchanged are still verified once and remembered by hash.
func New(cacheBytes int) *Preparer {
	return &Preparer{cache: newLRU(cacheBytes), verified: newKeySet(verifiedSources)}
}

// Prepare converts data into an image that satisfies target.
// Header identifies a source image without decoding its pixels.
type Header struct {
	// MediaType is the detected format, one of JPEG, PNG, GIF, or WebP.
	MediaType string
	// Width and Height are the stored dimensions, before EXIF orientation.
	Width, Height int
}

// Inspect reads the format and dimensions from data's header. It returns
// ErrUnsupportedFormat or ErrUndecodable when data is not a supported image.
func Inspect(data []byte) (Header, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return Header{}, ErrUnsupportedFormat
		}
		return Header{}, fmt.Errorf("%w: %v", ErrUndecodable, err)
	}
	mediaType := "image/" + format
	if !slices.Contains([]string{JPEG, PNG, GIF, WebP}, mediaType) {
		return Header{}, ErrUnsupportedFormat
	}
	if config.Width <= 0 || config.Height <= 0 {
		return Header{}, ErrUndecodable
	}
	return Header{MediaType: mediaType, Width: config.Width, Height: config.Height}, nil
}

func (p *Preparer) Prepare(data []byte, target Target) (Result, error) {
	header, err := Inspect(data)
	if err != nil {
		return Result{}, err
	}
	mediaType := header.MediaType
	config := image.Config{Width: header.Width, Height: header.Height}
	if int64(config.Width)*int64(config.Height) > MaxSourcePixels {
		return Result{}, ErrSourceTooLarge
	}

	orientation := readOrientation(data, mediaType)
	sourceWidth, sourceHeight := config.Width, config.Height
	if orientation >= 5 {
		sourceWidth, sourceHeight = sourceHeight, sourceWidth
	}
	width, height := sourceWidth, sourceHeight
	if target.Fit != nil {
		width, height = target.Fit(sourceWidth, sourceHeight)
		width = min(max(width, 1), sourceWidth)
		height = min(max(height, 1), sourceHeight)
	}

	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if width == sourceWidth && height == sourceHeight && orientation == 1 &&
		slices.Contains(target.Formats, mediaType) && withinLimit(len(data), target.MaxEncodedBytes) {
		// A valid header does not prove the image data decodes, and a
		// provider would reject a corrupt image, so decode it once.
		if !p.verified.has(digest) {
			if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
				return Result{}, fmt.Errorf("%w: %v", ErrUndecodable, err)
			}
			p.verified.add(digest)
		}
		return Result{
			MediaType: mediaType, Data: data, Width: width, Height: height,
			SourceWidth: sourceWidth, SourceHeight: sourceHeight,
		}, nil
	}

	key := cacheKey(digest, width, height, target)
	if cached, ok := p.cache.get(key); ok {
		cached.SourceWidth, cached.SourceHeight = sourceWidth, sourceHeight
		return cached, nil
	}

	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrUndecodable, err)
	}
	prepared := orient(decoded, orientation)
	if prepared.Bounds().Dx() != width || prepared.Bounds().Dy() != height {
		prepared = scale(prepared, width, height)
	}

	encodedType, encoded, err := encode(prepared, mediaType, target)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		MediaType: encodedType, Data: encoded, Width: width, Height: height,
		SourceWidth: sourceWidth, SourceHeight: sourceHeight, Changed: true,
	}
	p.cache.put(key, result)
	return result, nil
}

// EncodedLen returns the base64-encoded length of n bytes.
func EncodedLen(n int) int64 {
	return int64(base64.StdEncoding.EncodedLen(n))
}

func withinLimit(n int, limit int64) bool {
	return limit <= 0 || EncodedLen(n) <= limit
}

// encode writes img in the first accepted candidate format that fits the
// target's limit. The source format is preferred when the package can encode
// it, then PNG, then JPEG; JPEG is used only for opaque images.
func encode(img *image.NRGBA, sourceType string, target Target) (string, []byte, error) {
	var candidates []string
	if sourceType == JPEG || sourceType == PNG {
		candidates = append(candidates, sourceType)
	}
	candidates = append(candidates, PNG, JPEG)

	opaque := img.Opaque()
	tried := false
	for _, mediaType := range candidates {
		if !slices.Contains(target.Formats, mediaType) || (mediaType == JPEG && !opaque) {
			continue
		}
		tried = true
		var buf bytes.Buffer
		var err error
		if mediaType == JPEG {
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})
		} else {
			err = (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(&buf, img)
		}
		if err != nil {
			return "", nil, fmt.Errorf("encode %s: %w", mediaType, err)
		}
		if withinLimit(buf.Len(), target.MaxEncodedBytes) {
			return mediaType, buf.Bytes(), nil
		}
	}
	if !tried {
		return "", nil, ErrUnsupportedFormat
	}
	return "", nil, ErrEncodedLimit
}

// scale resamples img to width×height with Catmull-Rom filtering.
func scale(img *image.NRGBA, width, height int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	return dst
}

// orient returns img as NRGBA with EXIF orientation applied.
func orient(img image.Image, orientation int) *image.NRGBA {
	bounds := img.Bounds()
	src := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(src, src.Bounds(), img, bounds.Min, draw.Src)
	if orientation <= 1 || orientation > 8 {
		return src
	}
	w, h := bounds.Dx(), bounds.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch orientation {
			case 2: // mirror horizontal
				dx, dy = w-1-x, y
			case 3: // rotate 180
				dx, dy = w-1-x, h-1-y
			case 4: // mirror vertical
				dx, dy = x, h-1-y
			case 5: // transpose
				dx, dy = y, x
			case 6: // rotate 90 clockwise
				dx, dy = h-1-y, x
			case 7: // transverse
				dx, dy = h-1-y, w-1-x
			case 8: // rotate 90 counterclockwise
				dx, dy = y, w-1-x
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):dst.PixOffset(dx, dy)+4], src.Pix[src.PixOffset(x, y):src.PixOffset(x, y)+4])
		}
	}
	return dst
}

func cacheKey(digest string, width, height int, target Target) string {
	return strings.Join([]string{
		digest,
		strconv.Itoa(width), strconv.Itoa(height),
		strings.Join(target.Formats, ","),
		strconv.FormatInt(target.MaxEncodedBytes, 10),
	}, "|")
}
