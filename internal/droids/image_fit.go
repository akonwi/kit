package droids

import (
	"math"
	"regexp"
	"strconv"
)

// image_fit.go — provider sizing rules for ImagePolicy.Fit. Each returns the
// dimensions the provider processes for a source image, reproducing the
// provider's documented resize so prepared images are exactly what the model
// sees.

// anthropicImageFit reproduces Anthropic's reference resize: the largest
// aspect-preserving size whose 28 px-padded edges fit maxEdge and whose
// ⌈w/28⌉ × ⌈h/28⌉ visual-token count fits maxTokens. Python's round, which the
// reference uses, rounds half to even.
func anthropicImageFit(maxEdge, maxTokens int) ImageFit {
	const patch = 28
	fits := func(w, h int) bool {
		pw, ph := ceilDiv(w, patch), ceilDiv(h, patch)
		return pw*patch <= maxEdge && ph*patch <= maxEdge && pw*ph <= maxTokens
	}
	var fit ImageFit
	fit = func(w, h int) (int, int) {
		w, h = max(w, 1), max(h, 1)
		if fits(w, h) {
			return w, h
		}
		if h > w {
			rh, rw := fit(h, w)
			return rw, rh
		}
		aspect := float64(w) / float64(h)
		lo, hi := 1, w // lo always fits; hi never fits
		for lo+1 < hi {
			mid := (lo + hi) / 2
			if fits(mid, max(int(math.RoundToEven(float64(mid)/aspect)), 1)) {
				lo = mid
			} else {
				hi = mid
			}
		}
		return lo, max(int(math.RoundToEven(float64(lo)/aspect)), 1)
	}
	return fit
}

// openAIPatchFit reproduces the Responses API's patch-budget resize: fit the
// longest side within maxDimension, then shrink by area so the 32 px patch
// grid fits maxPatches, rounding the grid down so integer dimensions stay in
// budget.
func openAIPatchFit(maxDimension, maxPatches int) ImageFit {
	const patch = 32
	fits := func(w, h int) bool {
		return w <= maxDimension && h <= maxDimension && ceilDiv(w, patch)*ceilDiv(h, patch) <= maxPatches
	}
	return func(w, h int) (int, int) {
		w, h = max(w, 1), max(h, 1)
		if fits(w, h) {
			return w, h
		}
		dimensionScale := math.Min(float64(maxDimension)/float64(max(w, h)), 1)
		w = max(int(math.Round(float64(w)*dimensionScale)), 1)
		h = max(int(math.Round(float64(h)*dimensionScale)), 1)
		if fits(w, h) {
			return w, h
		}
		width, height := float64(w), float64(h)
		scale := math.Sqrt(patch * patch * float64(maxPatches) / width / height)
		wide, high := width*scale/patch, height*scale/patch
		scale *= math.Min(math.Floor(wide)/wide, math.Floor(high)/high)
		return max(int(math.Floor(width*scale)), 1), max(int(math.Floor(height*scale)), 1)
	}
}

// openAITileFit reproduces tile-based sizing: fit within 2048 × 2048, then
// scale the shortest side down to 768 px, rounding the other side down.
func openAITileFit(w, h int) (int, int) {
	w, h = max(w, 1), max(h, 1)
	if longest := max(w, h); longest > 2048 {
		scale := 2048 / float64(longest)
		w, h = max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)
	}
	if shortest := min(w, h); shortest > 768 {
		scale := 768 / float64(shortest)
		if w <= h {
			w, h = 768, max(int(float64(h)*scale), 1)
		} else {
			w, h = max(int(float64(w)*scale), 1), 768
		}
	}
	return w, h
}

// longEdgeImageFit bounds both sides by maxEdge, preserving aspect ratio and
// rounding down.
func longEdgeImageFit(maxEdge int) ImageFit {
	return func(w, h int) (int, int) {
		w, h = max(w, 1), max(h, 1)
		if longest := max(w, h); longest > maxEdge {
			scale := float64(maxEdge) / float64(longest)
			w, h = max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)
			w, h = min(w, maxEdge), min(h, maxEdge)
		}
		return w, h
	}
}

func ceilDiv(n, d int) int { return (n + d - 1) / d }

// claudeModelVersion matches claude-<family>-<major>[-<minor>][-<yyyymmdd>].
var claudeModelVersion = regexp.MustCompile(`^claude-[a-z]+-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?$`)

// anthropicHighResolution reports whether model is on Anthropic's
// high-resolution image tier, which applies to Claude 4.7 and later. IDs that
// do not carry a Claude version use the standard tier, which every model
// accepts.
func anthropicHighResolution(modelID string) bool {
	match := claudeModelVersion.FindStringSubmatch(modelID)
	if match == nil {
		return false
	}
	major, _ := strconv.Atoi(match[1])
	minor := 0
	if match[2] != "" {
		minor, _ = strconv.Atoi(match[2])
	}
	return major > 4 || (major == 4 && minor >= 7)
}
