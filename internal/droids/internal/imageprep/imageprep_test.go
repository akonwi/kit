package imageprep

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"os"
	"testing"
)

var allFormats = []string{JPEG, PNG, GIF, WebP}

func longEdge(limit int) func(w, h int) (int, int) {
	return func(w, h int) (int, int) {
		if w <= limit && h <= limit {
			return w, h
		}
		if w >= h {
			return limit, max(1, h*limit/w)
		}
		return max(1, w*limit/h), limit
	}
}

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeConfig(t *testing.T, data []byte) (image.Config, string) {
	t.Helper()
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return config, format
}

func TestConformingImageKeepsSourceBytes(t *testing.T) {
	source := encodePNG(t, solid(300, 200, color.NRGBA{R: 255, A: 255}))
	result, err := New(0).Prepare(source, Target{Formats: allFormats, Fit: longEdge(1000), MaxEncodedBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	want := Result{MediaType: PNG, Data: source, Width: 300, Height: 200, SourceWidth: 300, SourceHeight: 200}
	if !resultEqual(result, want) {
		t.Fatalf("result = %+v, want %+v", describe(result), describe(want))
	}
}

func TestOversizedImageIsDownscaledInSourceFormat(t *testing.T) {
	source := encodePNG(t, solid(3000, 2000, color.NRGBA{G: 255, A: 255}))
	result, err := New(0).Prepare(source, Target{Formats: allFormats, Fit: longEdge(1000)})
	if err != nil {
		t.Fatal(err)
	}
	if result.MediaType != PNG || result.Width != 1000 || result.Height != 666 ||
		result.SourceWidth != 3000 || result.SourceHeight != 2000 || !result.Changed || !result.Resized() {
		t.Fatalf("result = %+v", describe(result))
	}
	config, format := decodeConfig(t, result.Data)
	if format != "png" || config.Width != 1000 || config.Height != 666 {
		t.Fatalf("encoded = %s %dx%d, want png 1000x666", format, config.Width, config.Height)
	}
}

func TestFitNeverEnlarges(t *testing.T) {
	source := encodePNG(t, solid(40, 30, color.NRGBA{B: 255, A: 255}))
	result, err := New(0).Prepare(source, Target{Formats: allFormats, Fit: func(w, h int) (int, int) { return w * 4, h * 4 }})
	if err != nil {
		t.Fatal(err)
	}
	if result.Width != 40 || result.Height != 30 || result.Changed {
		t.Fatalf("result = %+v, want unchanged 40x30", describe(result))
	}
}

func TestUnacceptedFormatIsConvertedToPNG(t *testing.T) {
	webp, err := os.ReadFile("testdata/blue-purple-pink.lossy.webp")
	if err != nil {
		t.Fatal(err)
	}
	source, format := decodeConfig(t, webp)
	if format != "webp" {
		t.Fatalf("fixture format = %s", format)
	}

	unchanged, err := New(0).Prepare(webp, Target{Formats: allFormats})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.MediaType != WebP || unchanged.Changed {
		t.Fatalf("accepted WebP = %+v, want unchanged", describe(unchanged))
	}

	converted, err := New(0).Prepare(webp, Target{Formats: []string{JPEG, PNG}})
	if err != nil {
		t.Fatal(err)
	}
	config, format := decodeConfig(t, converted.Data)
	if converted.MediaType != PNG || format != "png" || !converted.Changed || converted.Resized() ||
		config.Width != source.Width || config.Height != source.Height {
		t.Fatalf("converted = %+v (%s %dx%d), want PNG %dx%d", describe(converted), format, config.Width, config.Height, source.Width, source.Height)
	}
}

func TestAnimatedGIFUsesFirstFrame(t *testing.T) {
	palette := color.Palette{color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}}
	red := image.NewPaletted(image.Rect(0, 0, 8, 8), palette)
	blue := image.NewPaletted(image.Rect(0, 0, 8, 8), palette)
	for i := range blue.Pix {
		blue.Pix[i] = 1
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{red, blue}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	result, err := New(0).Prepare(buf.Bytes(), Target{Formats: []string{PNG}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(decoded.At(4, 4)).(color.NRGBA); got != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("pixel = %v, want first-frame red", got)
	}
}

func TestEXIFOrientationIsApplied(t *testing.T) {
	// 40×20 source: left half red, right half blue. Orientation 6 rotates it
	// 90° clockwise to 20×40 with red on top and blue on the bottom.
	img := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := range 20 {
		for x := range 40 {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 20 {
				c = color.NRGBA{B: 255, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	source := withJPEGOrientation(t, encodeJPEG(t, img), 6)
	result, err := New(0).Prepare(source, Target{Formats: allFormats})
	if err != nil {
		t.Fatal(err)
	}
	if result.MediaType != JPEG || result.Width != 20 || result.Height != 40 ||
		result.SourceWidth != 20 || result.SourceHeight != 40 || !result.Changed || result.Resized() {
		t.Fatalf("result = %+v", describe(result))
	}
	decoded, err := jpeg.Decode(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatal(err)
	}
	top := color.NRGBAModel.Convert(decoded.At(10, 5)).(color.NRGBA)
	bottom := color.NRGBAModel.Convert(decoded.At(10, 35)).(color.NRGBA)
	if top.R < 200 || top.B > 60 || bottom.B < 200 || bottom.R > 60 {
		t.Fatalf("top = %v, bottom = %v; want red over blue", top, bottom)
	}
}

func TestOrientationReaders(t *testing.T) {
	base := encodePNG(t, solid(4, 2, color.NRGBA{A: 255}))
	tests := []struct {
		name      string
		data      []byte
		mediaType string
		want      int
	}{
		{"jpeg big-endian", withJPEGOrientation(t, encodeJPEG(t, solid(4, 2, color.NRGBA{A: 255})), 8), JPEG, 8},
		{"png eXIf little-endian", withPNGChunk(t, base, "eXIf", tiffWithOrientation(binary.LittleEndian, 3)), PNG, 3},
		{"webp EXIF", webpWithEXIF(tiffWithOrientation(binary.BigEndian, 5)), WebP, 5},
		{"no metadata", base, PNG, 1},
		{"out of range", withPNGChunk(t, base, "eXIf", tiffWithOrientation(binary.BigEndian, 9)), PNG, 1},
		{"truncated tiff", withPNGChunk(t, base, "eXIf", []byte("MM\x00*\x00\x00\x00\x08\x00")), PNG, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := readOrientation(test.data, test.mediaType); got != test.want {
				t.Fatalf("orientation = %d, want %d", got, test.want)
			}
		})
	}
}

func TestEncodedLimitFallsBackToJPEGForOpaqueImages(t *testing.T) {
	source := encodePNG(t, noise(256, 256, false))
	limit := EncodedLen(len(source)) - 1
	result, err := New(0).Prepare(source, Target{Formats: []string{JPEG, PNG}, MaxEncodedBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	if result.MediaType != JPEG || EncodedLen(len(result.Data)) > limit || result.Resized() {
		t.Fatalf("result = %+v (encoded %d, limit %d), want JPEG within limit", describe(result), EncodedLen(len(result.Data)), limit)
	}
}

func TestEncodedLimitRejectsTransparentImages(t *testing.T) {
	source := encodePNG(t, noise(256, 256, true))
	_, err := New(0).Prepare(source, Target{Formats: []string{JPEG, PNG}, MaxEncodedBytes: EncodedLen(len(source)) - 1})
	if !errors.Is(err, ErrEncodedLimit) {
		t.Fatalf("error = %v, want ErrEncodedLimit", err)
	}
}

func TestRejectedSources(t *testing.T) {
	bmp := append([]byte("BM"), make([]byte, 64)...)
	tests := []struct {
		name   string
		data   []byte
		target Target
		want   error
	}{
		{"unsupported format", bmp, Target{Formats: allFormats}, ErrUnsupportedFormat},
		{"corrupt png", encodePNG(t, solid(4, 4, color.NRGBA{A: 255}))[:40], Target{Formats: allFormats}, ErrUndecodable},
		{"too many pixels", pngWithDimensions(t, 6000, 5000), Target{Formats: allFormats}, ErrSourceTooLarge},
		{"no producible format", encodePNG(t, solid(4, 4, color.NRGBA{A: 255})), Target{Formats: []string{GIF}}, ErrUnsupportedFormat},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(0).Prepare(test.data, test.target); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPreparationIsDeterministicAndCached(t *testing.T) {
	source := encodeJPEG(t, noise(1200, 800, false))
	target := Target{Formats: allFormats, Fit: longEdge(500), MaxEncodedBytes: 1 << 20}

	first, err := New(0).Prepare(source, target)
	if err != nil {
		t.Fatal(err)
	}
	preparer := New(8 << 20)
	second, err := preparer.Prepare(source, target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Data, second.Data) {
		t.Fatal("independent preparations produced different bytes")
	}
	if preparer.cache.len() != 1 {
		t.Fatalf("cache entries = %d, want 1", preparer.cache.len())
	}
	cached, err := preparer.Prepare(source, target)
	if err != nil {
		t.Fatal(err)
	}
	if !resultEqual(cached, second) {
		t.Fatalf("cached = %+v, want %+v", describe(cached), describe(second))
	}
}

func TestLRUEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newLRU(10)
	cache.put("a", Result{Data: make([]byte, 4)})
	cache.put("b", Result{Data: make([]byte, 4)})
	cache.get("a")
	cache.put("c", Result{Data: make([]byte, 4)})
	_, hasA := cache.get("a")
	_, hasB := cache.get("b")
	_, hasC := cache.get("c")
	if !hasA || hasB || !hasC || cache.size != 8 {
		t.Fatalf("a=%v b=%v c=%v size=%d, want a and c retained at 8 bytes", hasA, hasB, hasC, cache.size)
	}
	cache.put("huge", Result{Data: make([]byte, 11)})
	if _, ok := cache.get("huge"); ok {
		t.Fatal("entry larger than capacity was cached")
	}
}

// noise returns a deterministic pseudo-random image that compresses poorly.
func noise(w, h int, transparent bool) *image.NRGBA {
	random := rand.New(rand.NewPCG(1, 2))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = uint8(random.Uint32()), uint8(random.Uint32()), uint8(random.Uint32())
		img.Pix[i+3] = 255
		if transparent {
			img.Pix[i+3] = uint8(random.Uint32())
		}
	}
	return img
}

func tiffWithOrientation(order binary.ByteOrder, orientation uint16) []byte {
	tiff := make([]byte, 8+2+12+4)
	if order == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], 1)
	order.PutUint16(tiff[10:], exifOrientationTag)
	order.PutUint16(tiff[12:], 3)
	order.PutUint32(tiff[14:], 1)
	order.PutUint16(tiff[18:], orientation)
	return tiff
}

func withJPEGOrientation(t *testing.T, data []byte, orientation uint16) []byte {
	t.Helper()
	payload := append(append([]byte(nil), exifHeader...), tiffWithOrientation(binary.BigEndian, orientation)...)
	segment := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	segment = append(segment, payload...)
	out := append([]byte(nil), data[:2]...)
	out = append(out, segment...)
	return append(out, data[2:]...)
}

func withPNGChunk(t *testing.T, data []byte, kind string, payload []byte) []byte {
	t.Helper()
	const headerEnd = 8 + 8 + 13 + 4 // signature, IHDR length/type, IHDR data, CRC
	out := append([]byte(nil), data[:headerEnd]...)
	out = append(out, pngChunk(kind, payload)...)
	return append(out, data[headerEnd:]...)
}

func pngChunk(kind string, payload []byte) []byte {
	chunk := make([]byte, 8, 12+len(payload))
	binary.BigEndian.PutUint32(chunk, uint32(len(payload)))
	copy(chunk[4:], kind)
	chunk = append(chunk, payload...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}

func pngWithDimensions(t *testing.T, w, h int) []byte {
	t.Helper()
	data := encodePNG(t, solid(1, 1, color.NRGBA{A: 255}))
	ihdr := data[8 : 8+8+13]
	binary.BigEndian.PutUint32(ihdr[8:], uint32(w))
	binary.BigEndian.PutUint32(ihdr[12:], uint32(h))
	binary.BigEndian.PutUint32(data[8+8+13:], crc32.ChecksumIEEE(ihdr[4:]))
	return data
}

func webpWithEXIF(tiff []byte) []byte {
	chunk := append([]byte("EXIF\x00\x00\x00\x00"), tiff...)
	binary.LittleEndian.PutUint32(chunk[4:], uint32(len(tiff)))
	if len(tiff)%2 == 1 {
		chunk = append(chunk, 0)
	}
	out := append([]byte("RIFF\x00\x00\x00\x00WEBP"), chunk...)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out
}

func resultEqual(a, b Result) bool {
	return a.MediaType == b.MediaType && bytes.Equal(a.Data, b.Data) &&
		a.Width == b.Width && a.Height == b.Height &&
		a.SourceWidth == b.SourceWidth && a.SourceHeight == b.SourceHeight && a.Changed == b.Changed
}

func describe(r Result) map[string]any {
	return map[string]any{
		"mediaType": r.MediaType, "bytes": len(r.Data), "size": [2]int{r.Width, r.Height},
		"source": [2]int{r.SourceWidth, r.SourceHeight}, "changed": r.Changed,
	}
}

func TestKeySetEvictsOldestKey(t *testing.T) {
	set := newKeySet(2)
	set.add("a")
	set.add("b")
	set.has("a")
	set.add("c")
	if !set.has("a") || set.has("b") || !set.has("c") {
		t.Fatalf("a=%v b=%v c=%v, want a and c retained", set.has("a"), set.has("b"), set.has("c"))
	}
}

func TestConcurrentPreparation(t *testing.T) {
	preparer := New(1 << 20)
	source := encodePNG(t, solid(400, 300, color.NRGBA{R: 10, G: 20, B: 30, A: 255}))
	target := Target{Formats: allFormats, Fit: longEdge(200)}
	done := make(chan Result, 8)
	for range 8 {
		go func() {
			result, err := preparer.Prepare(source, target)
			if err != nil {
				t.Error(err)
			}
			done <- result
		}()
	}
	first := <-done
	for range 7 {
		if got := <-done; !bytes.Equal(got.Data, first.Data) || got.Width != 200 || got.Height != 150 {
			t.Fatalf("concurrent result = %+v, want %+v", describe(got), describe(first))
		}
	}
}
