package terminalcolors

import (
	"testing"

	kittheme "github.com/akonwi/kit/internal/theme"
)

func TestParseReadsColorRepliesInEitherTerminator(t *testing.T) {
	received := []byte("\x1b]10;rgb:d8d8/dede/e9e9\x07" +
		"\x1b]11;rgb:28/2c/34\x1b\\" +
		"\x1b]4;1;rgb:e0/6c/75\x07" +
		"\x1b]4;7;rgb:f/8/0\x07" +
		"\x1b[?62;22c")
	got := Parse(received)
	want := Colors{
		Foreground: kittheme.Color{R: 0xd8, G: 0xde, B: 0xe9, A: 255},
		Background: kittheme.Color{R: 0x28, G: 0x2c, B: 0x34, A: 255},
		Palette: []kittheme.Color{
			{},
			{R: 0xe0, G: 0x6c, B: 0x75, A: 255},
			{}, {}, {}, {}, {},
			{R: 0xff, G: 0x88, B: 0x00, A: 255},
		},
	}
	if got.Foreground != want.Foreground || got.Background != want.Background {
		t.Fatalf("defaults = %+v / %+v, want %+v / %+v", got.Foreground, got.Background, want.Foreground, want.Background)
	}
	for index := range want.Palette {
		if got.Palette[index] != want.Palette[index] {
			t.Fatalf("palette[%d] = %+v, want %+v", index, got.Palette[index], want.Palette[index])
		}
	}
}

func TestParseWithoutRepliesReportsNoColors(t *testing.T) {
	got := Parse([]byte("\x1b[?1;2c"))
	if got.Foreground.A != 0 || got.Background.A != 0 || len(got.Palette) != 8 {
		t.Fatalf("Parse = %+v, want no colors and an eight-entry palette", got)
	}
	for index, color := range got.Palette {
		if color.A != 0 {
			t.Fatalf("palette[%d] = %+v, want unreported", index, color)
		}
	}
}
