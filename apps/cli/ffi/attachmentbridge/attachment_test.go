package attachmentbridge

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func encodePNG(t *testing.T, width, height int, fill color.Color) []byte {
	t.Helper()
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			source.Set(x, y, fill)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestDecodeScalesDownKeepingProportions(t *testing.T) {
	pixels, err := Decode(bytes.NewReader(encodePNG(t, 400, 100, color.NRGBA{R: 255, A: 255})), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if pixels.Width != 100 || pixels.Height != 25 || len(pixels.RGBA) != 100*25*4 {
		t.Fatalf("scaled to %dx%d with %d bytes", pixels.Width, pixels.Height, len(pixels.RGBA))
	}
	if got := pixels.RGBA[:4]; !bytes.Equal(got, []byte{255, 0, 0, 255}) {
		t.Fatalf("first pixel = %v", got)
	}
}

func TestDecodeKeepsSmallImagesAndStraightAlpha(t *testing.T) {
	pixels, err := Decode(bytes.NewReader(encodePNG(t, 2, 3, color.NRGBA{R: 200, G: 100, A: 128})), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if pixels.Width != 2 || pixels.Height != 3 {
		t.Fatalf("size = %dx%d", pixels.Width, pixels.Height)
	}
	if got := pixels.RGBA[:4]; !bytes.Equal(got, []byte{200, 100, 0, 128}) {
		t.Fatalf("first pixel = %v, want straight alpha", got)
	}
}

func TestDecodeReadsGIF(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	source := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	var encoded bytes.Buffer
	if err := gif.Encode(&encoded, source, nil); err != nil {
		t.Fatal(err)
	}
	if pixels, err := Decode(&encoded, 10, 10); err != nil || pixels.Width != 4 {
		t.Fatalf("gif = %+v, %v", pixels, err)
	}
}

func TestDecodeRejectsTextAndBadBounds(t *testing.T) {
	if _, err := Decode(bytes.NewReader([]byte("hello")), 10, 10); err == nil {
		t.Fatal("text decoded as an image")
	}
	if _, err := Decode(bytes.NewReader(encodePNG(t, 1, 1, color.Black)), 0, 10); err == nil {
		t.Fatal("empty bounds accepted")
	}
}

func TestFitKeepsAtLeastOnePixel(t *testing.T) {
	if width, height := fit(1000, 1, 10, 10); width != 10 || height != 1 {
		t.Fatalf("fit = %dx%d", width, height)
	}
	if width, height := fit(1, 1000, 10, 10); width != 1 || height != 10 {
		t.Fatalf("fit = %dx%d", width, height)
	}
}
