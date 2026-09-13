package tui

import (
	"image"
	"image/color"
	"testing"

	"go.rockorager.dev/vaxis/ui"
)

func TestRenderHalfBlockImageFitsCellBoundsAndPreservesAspectRatio(t *testing.T) {
	t.Parallel()

	wide := image.NewRGBA(image.Rect(0, 0, 400, 100))
	got := renderHalfBlockImage(wide, 72, 12, color.RGBA{})
	if got.Width != 72 || len(got.Rows) != 9 {
		t.Fatalf("wide raster size = %dx%d cells, want 72x9", got.Width, len(got.Rows))
	}

	tall := image.NewRGBA(image.Rect(0, 0, 100, 400))
	got = renderHalfBlockImage(tall, 72, 12, color.RGBA{})
	if got.Width != 6 || len(got.Rows) != 12 {
		t.Fatalf("tall raster size = %dx%d cells, want 6x12", got.Width, len(got.Rows))
	}
}

func TestRenderHalfBlockImagePairsVerticalPixels(t *testing.T) {
	t.Parallel()

	source := image.NewRGBA(image.Rect(0, 0, 2, 2))
	source.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	source.SetRGBA(1, 0, color.RGBA{G: 255, A: 255})
	source.SetRGBA(0, 1, color.RGBA{B: 255, A: 255})
	source.SetRGBA(1, 1, color.RGBA{R: 255, G: 255, A: 255})

	got := renderHalfBlockImage(source, 2, 1, color.RGBA{A: 255})
	if got.Width != 2 || len(got.Rows) != 1 || len(got.Rows[0]) != 2 {
		t.Fatalf("raster dimensions = %#v", got)
	}
	if got.Rows[0][0].Top != (color.RGBA{R: 255, A: 255}) || got.Rows[0][0].Bottom != (color.RGBA{B: 255, A: 255}) {
		t.Fatalf("first cell = %#v", got.Rows[0][0])
	}
	if got.Rows[0][1].Top != (color.RGBA{G: 255, A: 255}) || got.Rows[0][1].Bottom != (color.RGBA{R: 255, G: 255, A: 255}) {
		t.Fatalf("second cell = %#v", got.Rows[0][1])
	}
}

func TestHalfBlockRasterWidgetPaintsUpperBlockColors(t *testing.T) {
	t.Parallel()

	raster := halfBlockRaster{Width: 1, Rows: [][]halfBlockCell{{{
		Top: color.RGBA{R: 1, G: 2, B: 3, A: 255}, Bottom: color.RGBA{R: 4, G: 5, B: 6, A: 255},
	}}}}
	column, ok := halfBlockRasterWidget(raster).(ui.Flex)
	if !ok || column.Axis != ui.Vertical || len(column.Children) != 1 {
		t.Fatalf("widget = %#v", column)
	}
	row, ok := column.Children[0].(ui.RichText)
	if !ok || len(row.Spans) != 1 {
		t.Fatalf("row = %#v", column.Children[0])
	}
	span := row.Spans[0]
	if span.Text != "▀" || span.Style.Foreground != ui.RGB(1, 2, 3) || span.Style.Background != ui.RGB(4, 5, 6) {
		t.Fatalf("painted span = %#v", span)
	}
}

func TestRenderHalfBlockImageCompositesTransparencyAndPadsOddHeight(t *testing.T) {
	t.Parallel()

	background := color.RGBA{R: 20, G: 40, B: 60, A: 255}
	source := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 220, G: 40, B: 20, A: 128})

	got := renderHalfBlockImage(source, 1, 1, background)
	cell := got.Rows[0][0]
	if cell.Bottom != background {
		t.Fatalf("odd-height bottom = %#v, want background %#v", cell.Bottom, background)
	}
	if cell.Top.A != 255 || cell.Top.R < 119 || cell.Top.R > 121 || cell.Top.G != 40 || cell.Top.B < 39 || cell.Top.B > 41 {
		t.Fatalf("composited top = %#v, want approximately rgba(120,40,40,255)", cell.Top)
	}
}

func TestRenderHalfBlockImageRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	for _, got := range []halfBlockRaster{
		renderHalfBlockImage(nil, 72, 12, color.RGBA{}),
		renderHalfBlockImage(image.NewRGBA(image.Rectangle{}), 72, 12, color.RGBA{}),
		renderHalfBlockImage(image.NewRGBA(image.Rect(0, 0, 1, 1)), 0, 12, color.RGBA{}),
	} {
		if got.Width != 0 || len(got.Rows) != 0 {
			t.Fatalf("empty render = %#v", got)
		}
	}
}
