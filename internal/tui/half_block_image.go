package tui

import (
	"image"
	"image/color"
	"math"

	"go.rockorager.dev/vaxis/ui"
	"golang.org/x/image/draw"
)

// halfBlockRaster is a terminal-cell projection of an image. Each cell stores
// the colors painted above and below an upper-half-block glyph.
type halfBlockRaster struct {
	Width int
	Rows  [][]halfBlockCell
}

type halfBlockCell struct {
	Top    color.RGBA
	Bottom color.RGBA
}

// halfBlockRasterWidget turns a prepared raster into ordinary selectable UI
// cells. Rendering is intentionally separate from decoding/scaling so callers
// can do that work off the UI event loop and cache the result.
func halfBlockRasterWidget(raster halfBlockRaster) ui.Widget {
	rows := make([]ui.Widget, 0, len(raster.Rows))
	for _, cells := range raster.Rows {
		spans := make([]ui.TextSpan, 0, len(cells))
		for _, cell := range cells {
			spans = append(spans, ui.TextSpan{
				Text: "▀",
				Style: ui.Style{
					Foreground: ui.RGB(cell.Top.R, cell.Top.G, cell.Top.B),
					Background: ui.RGB(cell.Bottom.R, cell.Bottom.G, cell.Bottom.B),
				},
			})
		}
		rows = append(rows, ui.RichText{Spans: spans, MaxLines: 1, Overflow: ui.TextOverflowClip})
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStart, MainAxisSize: ui.MainAxisSizeMin, Children: rows}
}

// renderHalfBlockImage fits src inside maxColumns by maxRows terminal cells.
// A terminal cell represents two vertical image samples. The image is
// resampled with an area-weighted kernel so every source pixel contributes to
// the sample covering it, then transparent pixels are composited against
// background before the raster reaches the painter.
func renderHalfBlockImage(src image.Image, maxColumns, maxRows int, background color.RGBA) halfBlockRaster {
	if src == nil || maxColumns <= 0 || maxRows <= 0 {
		return halfBlockRaster{}
	}
	bounds := src.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return halfBlockRaster{}
	}

	maxPixelRows := maxRows * 2
	scale := math.Min(float64(maxColumns)/float64(sourceWidth), float64(maxPixelRows)/float64(sourceHeight))
	width := max(1, min(maxColumns, int(math.Round(float64(sourceWidth)*scale))))
	pixelRows := max(1, min(maxPixelRows, int(math.Round(float64(sourceHeight)*scale))))
	rows := (pixelRows + 1) / 2
	raster := halfBlockRaster{Width: width, Rows: make([][]halfBlockCell, rows)}

	scaled := resampleImage(src, width, pixelRows)
	for row := range rows {
		line := make([]halfBlockCell, width)
		for column := range width {
			line[column].Top = compositeImageColor(scaled.RGBAAt(column, row*2), background)
			if bottomRow := row*2 + 1; bottomRow < pixelRows {
				line[column].Bottom = compositeImageColor(scaled.RGBAAt(column, bottomRow), background)
			} else {
				line[column].Bottom = background
			}
		}
		raster.Rows[row] = line
	}
	return raster
}

// resampleImage scales src to exactly width by height pixels. The result is
// alpha-premultiplied, matching what color.Color.RGBA reports, so averaging
// partially transparent pixels stays correct.
func resampleImage(src image.Image, width, height int) *image.RGBA {
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := src.Bounds()
	if bounds.Dx() == width && bounds.Dy() == height {
		draw.Copy(target, image.Point{}, src, bounds, draw.Src, nil)
		return target
	}
	// CatmullRom widens its kernel when shrinking, so each output pixel is a
	// weighted average of the source region it covers rather than a single
	// nearest sample. It runs off the UI loop and the result is cached.
	draw.CatmullRom.Scale(target, target.Bounds(), src, bounds, draw.Src, nil)
	return target
}

func compositeImageColor(foreground color.Color, background color.RGBA) color.RGBA {
	r, g, b, a := foreground.RGBA()
	if a == 0xffff {
		return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff}
	}
	inverse := uint64(0xffff - a)
	blend := func(value uint32, behind uint8) uint8 {
		// color.Color.RGBA returns alpha-premultiplied components.
		combined := uint64(value) + uint64(behind)*0x101*inverse/0xffff
		return uint8(min(uint64(0xffff), combined) >> 8)
	}
	return color.RGBA{
		R: blend(r, background.R),
		G: blend(g, background.G),
		B: blend(b, background.B),
		A: 0xff,
	}
}
