/**
 * Centralized Unicode glyph constants for TUI affordances.
 *
 * All glyphs used in the UI should be defined here. Components import
 * named constants rather than scattering inline Unicode strings.
 *
 * Constants are named after what the glyph IS, not how it's used.
 */

// ── Marks ───────────────────────────────────────────────────────────

/** ✓ */
export const CHECK = "✓";
/** ✗ */
export const CROSS = "✗";
/** ⊘ */
export const CIRCLE_SLASH = "⊘";
/** ✎ */
export const PENCIL = "✎";
/** ◆ */
export const DIAMOND = "◆";

// ── Circles ─────────────────────────────────────────────────────────

/** ● */
export const CIRCLE_FILLED = "●";
/** ○ */
export const CIRCLE_EMPTY = "○";

// ── Triangles ───────────────────────────────────────────────────────

/** ▸ */
export const TRIANGLE_RIGHT = "▸";
/** ▾ */
export const TRIANGLE_DOWN = "▾";
/** ▲ */
export const TRIANGLE_UP = "▲";

// ── Arrows ──────────────────────────────────────────────────────────

/** ↑ */
export const ARROW_UP = "↑";

// ── Lines ───────────────────────────────────────────────────────────

/** ─ */
export const HORIZONTAL_LINE = "─";
/** ━ */
export const HEAVY_LINE = "━";
/** │ */
export const VERTICAL_LINE = "│";
/** ▎ */
export const THIN_BAR = "▎";
/** ┆ */
export const DASHED_VERTICAL = "┆";

// ── Punctuation / separators ────────────────────────────────────────

/** … */
export const ELLIPSIS = "…";
/** · */
export const MIDDLE_DOT = "·";
/** › */
export const CHEVRON_RIGHT = "›";
/** × */
export const TIMES = "×";

// ── Blocks ──────────────────────────────────────────────────────────

/** █ */
export const FULL_BLOCK = "█";

// ── Tree connectors ─────────────────────────────────────────────────

/** ├─  */
export const TREE_BRANCH = "├─ ";
/** └─  */
export const TREE_CORNER = "└─ ";

// ── Spinner ─────────────────────────────────────────────────────────

/** Braille spinner frames (80ms per frame) */
export const SPINNER_FRAMES = [
	"⠋",
	"⠙",
	"⠹",
	"⠸",
	"⠼",
	"⠴",
	"⠦",
	"⠧",
	"⠇",
	"⠏",
];
