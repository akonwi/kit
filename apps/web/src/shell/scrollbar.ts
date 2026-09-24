/**
 * Compute a boolean track for a scrollbar thumb.
 * Returns null when the content fits without scrolling.
 */
export function computeScrollbar(
	total: number,
	visible: number,
	offset: number,
): boolean[] | null {
	if (total <= visible) return null;
	const thumbSize = Math.max(1, Math.round((visible / total) * visible));
	const maxOffset = total - visible;
	const thumbOffset = Math.round((offset / maxOffset) * (visible - thumbSize));
	const track: boolean[] = [];
	for (let i = 0; i < visible; i++) {
		track.push(i >= thumbOffset && i < thumbOffset + thumbSize);
	}
	return track;
}
