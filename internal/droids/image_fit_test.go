package droids

import "testing"

type fitCase struct {
	w, h, wantW, wantH int
}

func assertFit(t *testing.T, name string, fit ImageFit, cases []fitCase) {
	t.Helper()
	for _, c := range cases {
		if w, h := fit(c.w, c.h); w != c.wantW || h != c.wantH {
			t.Errorf("%s(%d, %d) = %dx%d, want %dx%d", name, c.w, c.h, w, h, c.wantW, c.wantH)
		}
	}
}

// Expected values are the worked examples in Anthropic's vision and
// vision-coordinates documentation.
func TestAnthropicImageFitMatchesDocumentedExamples(t *testing.T) {
	assertFit(t, "standard", anthropicImageFit(1568, 1568), []fitCase{
		{200, 200, 200, 200},
		{1000, 1000, 1000, 1000},
		{1092, 1092, 1092, 1092},
		{1920, 1080, 1456, 819},
		// The documentation table lists 1269x952; its reference
		// implementation, which the docs direct callers to use, yields
		// 1270x952. Both cost 1,564 visual tokens.
		{2000, 1500, 1270, 952},
		{3840, 2160, 1456, 819},
		{1075, 1520, 924, 1307}, // token budget binds although both sides fit the edge
	})
	assertFit(t, "high-resolution", anthropicImageFit(2576, 4784), []fitCase{
		{1920, 1080, 1920, 1080},
		{2000, 1500, 2000, 1500},
		{3840, 2160, 2576, 1449},
		{1075, 1520, 1075, 1520},
	})
}

func TestAnthropicHighResolutionTier(t *testing.T) {
	for id, want := range map[string]bool{
		"claude-opus-4-7":            true,
		"claude-opus-4-8":            true,
		"claude-opus-5":              true,
		"claude-opus-5-5":            true,
		"claude-sonnet-5":            true,
		"claude-fable-5-1":           true,
		"claude-opus-4-6":            false,
		"claude-sonnet-4-6":          false,
		"claude-haiku-4-5":           false,
		"claude-haiku-4-5-20251001":  false,
		"claude-sonnet-4-5-20250929": false,
		"minimax-m3":                 false,
	} {
		if got := anthropicHighResolution(id); got != want {
			t.Errorf("anthropicHighResolution(%q) = %v, want %v", id, got, want)
		}
	}
}

// Expected values follow the OpenAI images and vision guide's examples and
// the patch-budget rules it documents.
func TestOpenAIImageFits(t *testing.T) {
	assertFit(t, "gpt-6-astra high", openAIHighDetailFit("gpt-6-astra"), []fitCase{
		{1024, 1024, 1024, 1024},
		{2048, 2048, 1600, 1600},
		{4096, 512, 4096, 512},
	})
	assertFit(t, "default high", openAIHighDetailFit("gpt-6-sol"), []fitCase{
		{1920, 1080, 1920, 1080}, // 60 × 34 = 2,040 patches
		{4096, 512, 2048, 256},
		{2880, 1800, 1996, 1248}, // 2048x1280 is 2,560 patches; 63 × 39 = 2,457

	})
	assertFit(t, "gpt-5.2", openAIHighDetailFit("gpt-5.2-pro"), []fitCase{
		{2880, 1800, 2048, 1280}, // 64 × 40 = 2,560 patches fits 6,144
	})
	assertFit(t, "tile", openAIHighDetailFit("gpt-4o-mini"), []fitCase{
		{1024, 1024, 768, 768},
		{4096, 2048, 1536, 768},
		{2048, 4096, 768, 1536},
		{512, 512, 512, 512},
	})
	for _, id := range []string{"gpt-4.1-mini", "gpt-5.4", "gpt-5.6-sol", "gpt-6-luna"} {
		w, h := openAIHighDetailFit(id)(4000, 4000)
		if ceilDiv(w, 32)*ceilDiv(h, 32) > 6_144 || w > 2048 || h > 2048 {
			t.Errorf("%s fit 4000x4000 = %dx%d, exceeds its documented high-detail limits", id, w, h)
		}
	}
}

func TestOpenAIPatchFitStaysWithinBudget(t *testing.T) {
	fit := openAIPatchFit(2048, 2500)
	for _, size := range [][2]int{{1, 9000}, {9000, 1}, {3001, 1999}, {2049, 2049}, {1601, 1601}, {5000, 37}} {
		w, h := fit(size[0], size[1])
		if w < 1 || h < 1 || w > 2048 || h > 2048 || ceilDiv(w, 32)*ceilDiv(h, 32) > 2500 {
			t.Errorf("fit(%d, %d) = %dx%d, outside 2048 px and 2,500 patches", size[0], size[1], w, h)
		}
	}
}

func TestLongEdgeImageFit(t *testing.T) {
	assertFit(t, "2000", longEdgeImageFit(2000), []fitCase{
		{2576, 1449, 2000, 1125},
		{1449, 2576, 1125, 2000},
		{1456, 819, 1456, 819},
	})
}
