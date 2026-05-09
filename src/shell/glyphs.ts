/**
 * Centralized Unicode glyph constants for TUI affordances.
 *
 * All glyphs used in the UI should be defined here. Components import
 * named constants rather than scattering inline Unicode strings.
 */

// ── Status indicators ───────────────────────────────────────────────

/** Success / completed */
export const GLYPH_SUCCESS = "✓";
/** Error / failure */
export const GLYPH_ERROR = "✗";
/** Aborted / cancelled */
export const GLYPH_ABORTED = "⊘";
/** Active / on / dirty */
export const GLYPH_ACTIVE = "●";
/** Inactive / off / clean / idle */
export const GLYPH_INACTIVE = "○";

// ── Expand / collapse ───────────────────────────────────────────────

/** Collapsed (right-pointing) */
export const GLYPH_COLLAPSED = "▸";
/** Expanded (down-pointing) */
export const GLYPH_EXPANDED = "▾";

// ── Structural ──────────────────────────────────────────────────────

/** Horizontal rule / border */
export const GLYPH_HORIZONTAL = "─";
/** Heavy horizontal (accent underline) */
export const GLYPH_HORIZONTAL_HEAVY = "━";
/** Middle dot separator */
export const GLYPH_SEPARATOR = "·";
/** Focused item indicator */
export const GLYPH_FOCUS = "›";
/** Dismiss / close */
export const GLYPH_DISMISS = "×";

// ── Review ──────────────────────────────────────────────────────────

/** Edit / note marker */
export const GLYPH_EDIT = "✎";
/** Saved comment marker */
export const GLYPH_COMMENT = "◆";
/** Range anchor bar */
export const GLYPH_RANGE_BAR = "▎";
/** Multi-line note continuation */
export const GLYPH_NOTE_CONTINUATION = "┆";

// ── Scrollbar ───────────────────────────────────────────────────────

/** Scrollbar thumb (filled) */
export const GLYPH_SCROLL_THUMB = "█";
/** Scrollbar track */
export const GLYPH_SCROLL_TRACK = "│";

// ── Tree ────────────────────────────────────────────────────────────

/** Tree branch (non-last child) */
export const GLYPH_TREE_BRANCH = "├─ ";
/** Tree branch (last child) */
export const GLYPH_TREE_LAST = "└─ ";

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
