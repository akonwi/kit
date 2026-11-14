// The string handling Kit's Mermaid reference does in Go, for the parts of
// it the browser ports (apps/cli/tui/mermaid, ADR 0046): whitespace as Go's
// unicode.IsSpace sees it, and text cut to a number of terminal cells.

const SPACE = /[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]/u;

/** Why a diagram can't be read, worded for the reader. */
export class NotDrawn extends Error {}

export function isSpace(char: string): boolean {
  return SPACE.test(char);
}

/** value without the whitespace around it. */
export function trimSpace(value: string): string {
  const chars = [...value];
  let start = 0;
  let end = chars.length;
  while (start < end && isSpace(chars[start] ?? "")) start++;
  while (end > start && isSpace(chars[end - 1] ?? "")) end--;
  return chars.slice(start, end).join("");
}

/** value split around runs of whitespace. */
export function fields(value: string): string[] {
  const result: string[] = [];
  let current = "";
  for (const char of value) {
    if (isSpace(char)) {
      if (current) result.push(current);
      current = "";
    } else current += char;
  }
  if (current) result.push(current);
  return result;
}

/** value without any of cutset's characters at either end. */
export function trim(value: string, cutset: string): string {
  const chars = [...value];
  let start = 0;
  let end = chars.length;
  while (start < end && cutset.includes(chars[start] ?? "")) start++;
  while (end > start && cutset.includes(chars[end - 1] ?? "")) end--;
  return chars.slice(start, end).join("");
}

export function trimPrefix(value: string, prefix: string): string {
  return value.startsWith(prefix) ? value.slice(prefix.length) : value;
}

export function trimSuffix(value: string, suffix: string): string {
  return suffix && value.endsWith(suffix) ? value.slice(0, -suffix.length) : value;
}

/** Lowercases character by character. */
export function toLower(value: string): string {
  return [...value]
    .map((char) => (char.toLowerCase().length === char.length ? char.toLowerCase() : char))
    .join("");
}

export function toUpper(value: string): string {
  return [...value]
    .map((char) => (char.toUpperCase().length === char.length ? char.toUpperCase() : char))
    .join("");
}

/** The index of the first of chars in value, or -1. */
export function indexAny(value: string, chars: string): number {
  for (let index = 0; index < value.length; index++) if (chars.includes(value[index] ?? "")) return index;
  return -1;
}

/** A decimal integer with an optional sign, within a 64-bit integer's range, as Go's strconv.Atoi reads it. */
export function atoi(value: string): bigint | undefined {
  if (!/^[+-]?\d+$/.test(value)) return undefined;
  const number = BigInt(value);
  return BigInt.asIntN(64, number) === number ? number : undefined;
}

/** value's size in UTF-8 bytes. */
export function byteSize(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

// MARK: Terminal cells

const graphemeSegmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });

const MARK = /^[\p{Mn}\p{Me}]$/u;
const FORMAT = /^\p{Cf}$/u;
const SPACING_MARK = /^\p{Mc}$/u;
const EMOJI = /^\p{Emoji}$/u;
const EMOJI_PRESENTATION = /^\p{Emoji_Presentation}$/u;

/** East Asian Wide and Fullwidth ranges outside emoji presentation. */
const WIDE: [number, number][] = [
  [0x1100, 0x115f],
  [0x231a, 0x231b],
  [0x2329, 0x232a],
  [0x23e9, 0x23ec],
  [0x23f0, 0x23f0],
  [0x23f3, 0x23f3],
  [0x25fd, 0x25fe],
  [0x2614, 0x2615],
  [0x2630, 0x2637],
  [0x2648, 0x2653],
  [0x267f, 0x267f],
  [0x268a, 0x268f],
  [0x2693, 0x2693],
  [0x26a1, 0x26a1],
  [0x26aa, 0x26ab],
  [0x26bd, 0x26be],
  [0x26c4, 0x26c5],
  [0x26ce, 0x26ce],
  [0x26d4, 0x26d4],
  [0x26ea, 0x26ea],
  [0x26f2, 0x26f3],
  [0x26f5, 0x26f5],
  [0x26fa, 0x26fa],
  [0x26fd, 0x26fd],
  [0x2705, 0x2705],
  [0x270a, 0x270b],
  [0x2728, 0x2728],
  [0x274c, 0x274c],
  [0x274e, 0x274e],
  [0x2753, 0x2755],
  [0x2757, 0x2757],
  [0x2795, 0x2797],
  [0x27b0, 0x27b0],
  [0x27bf, 0x27bf],
  [0x2b1b, 0x2b1c],
  [0x2b50, 0x2b50],
  [0x2b55, 0x2b55],
  [0x2e80, 0x303e],
  [0x3041, 0x3247],
  [0x3250, 0x33ff],
  [0x3400, 0x4dff],
  [0x4e00, 0x9fff],
  [0xa000, 0xa4cf],
  [0xa960, 0xa97f],
  [0xac00, 0xd7a3],
  [0xf900, 0xfaff],
  [0xfe10, 0xfe19],
  [0xfe30, 0xfe6f],
  [0xff00, 0xff60],
  [0xffe0, 0xffe6],
  [0x16fe0, 0x16fe4],
  [0x16ff0, 0x16ff1],
  [0x17000, 0x18cff],
  [0x18d00, 0x18d08],
  [0x1aff0, 0x1b2ff],
  [0x1d300, 0x1d356],
  [0x1d360, 0x1d376],
  [0x1f004, 0x1f004],
  [0x1f0cf, 0x1f0cf],
  [0x1f18e, 0x1f18e],
  [0x1f191, 0x1f19a],
  [0x1f200, 0x1f202],
  [0x1f210, 0x1f23b],
  [0x1f240, 0x1f248],
  [0x1f250, 0x1f251],
  [0x1f260, 0x1f265],
  [0x1f300, 0x1f320],
  [0x1f32d, 0x1f335],
  [0x1f337, 0x1f37c],
  [0x1f37e, 0x1f393],
  [0x1f3a0, 0x1f3ca],
  [0x1f3cf, 0x1f3d3],
  [0x1f3e0, 0x1f3f0],
  [0x1f3f4, 0x1f3f4],
  [0x1f3f8, 0x1f43e],
  [0x1f440, 0x1f440],
  [0x1f442, 0x1f4fc],
  [0x1f4ff, 0x1f53d],
  [0x1f54b, 0x1f54e],
  [0x1f550, 0x1f567],
  [0x1f57a, 0x1f57a],
  [0x1f595, 0x1f596],
  [0x1f5a4, 0x1f5a4],
  [0x1f5fb, 0x1f64f],
  [0x1f680, 0x1f6c5],
  [0x1f6cc, 0x1f6cc],
  [0x1f6d0, 0x1f6d2],
  [0x1f6d5, 0x1f6d7],
  [0x1f6dc, 0x1f6df],
  [0x1f6eb, 0x1f6ec],
  [0x1f6f4, 0x1f6fc],
  [0x1f7e0, 0x1f7eb],
  [0x1f7f0, 0x1f7f0],
  [0x1f90c, 0x1f93a],
  [0x1f93c, 0x1f945],
  [0x1f947, 0x1f9ff],
  [0x1fa70, 0x1faff],
  [0x20000, 0x2fffd],
  [0x30000, 0x3fffd],
];

const isWide = (value: number) => WIDE.some(([low, high]) => value >= low && value <= high);
const isRegional = (value: number) => value >= 0x1f1e6 && value <= 0x1f1ff;

function zeroWidth(char: string, value: number): boolean {
  if (MARK.test(char) || FORMAT.test(char)) return true;
  return (
    (value >= 0x1160 && value <= 0x11ff) ||
    (value >= 0xd7b0 && value <= 0xd7ff) ||
    (value >= 0xfe00 && value <= 0xfe0f) ||
    (value >= 0xe0100 && value <= 0xe01ef) ||
    value === 0x200b
  );
}

/** Whether a character adds no width after the first in a grapheme. */
function zeroInGrapheme(char: string, value: number): boolean {
  return zeroWidth(char, value) || SPACING_MARK.test(char) || (value >= 0xe0000 && value <= 0xe007f);
}

function isExtend(char: string, value: number): boolean {
  if (MARK.test(char) || SPACING_MARK.test(char)) return true;
  return (
    (value >= 0xfe00 && value <= 0xfe0f) ||
    (value >= 0xe0020 && value <= 0xe007f) ||
    (value >= 0x1f3fb && value <= 0x1f3ff)
  );
}

const isPictographic = (char: string, value: number) =>
  (value > 0xff && EMOJI.test(char)) || value === 0xa9 || value === 0xae;

/** A character's width on its own. */
function standalone(char: string): number {
  const value = char.codePointAt(0) ?? 0;
  if (value >= 0x20 && value < 0x7f) return 1;
  if (value < 0x20 || (value >= 0x7f && value < 0xa0)) return 0;
  if (zeroWidth(char, value)) return 0;
  // A regional indicator only fills two cells as half of a flag.
  if (isRegional(value)) return 1;
  if (isWide(value) || EMOJI_PRESENTATION.test(char)) return 2;
  return 1;
}

/** The cells one grapheme cluster takes, under Ghostty's rules. */
function graphemeWidth(cluster: string): number {
  const chars = [...cluster];
  const first = chars[0];
  if (first === undefined) return 0;
  if (chars.length === 1) return standalone(first);
  const firstValue = first.codePointAt(0) ?? 0;
  let width = zeroInGrapheme(first, firstValue) ? 0 : standalone(first);
  let previous = first;
  let pictographic = isPictographic(first, firstValue);
  let regional = isRegional(firstValue);
  let index = 1;
  while (index < chars.length) {
    const char = chars[index] ?? "";
    const value = char.codePointAt(0) ?? 0;
    if (value === 0xfe0f) {
      if (EMOJI.test(previous)) width = 2;
    } else if (value === 0xfe0e) {
      if (EMOJI.test(previous)) width = 1;
    } else if (value === 0x200d && pictographic && index + 1 < chars.length) {
      // The pictograph a joiner adds belongs to the emoji before it.
      index += 1;
      previous = chars[index] ?? "";
      pictographic = isPictographic(previous, previous.codePointAt(0) ?? 0);
      index += 1;
      continue;
    } else if (value >= 0x1f3fb && value <= 0x1f3ff) {
      width = 2;
    } else if (regional && isRegional(value)) {
      width = 2;
    } else if (!zeroInGrapheme(char, value)) {
      width += standalone(char);
    }
    if (!isExtend(char, value)) {
      pictographic = isPictographic(char, value);
      regional = isRegional(value) && !regional;
    }
    previous = char;
    index += 1;
  }
  return width;
}

/** value's graphemes in order, each with its cell width; a tab is eight spaces. */
function graphemes(value: string): { text: string; width: number }[] {
  const result: { text: string; width: number }[] = [];
  for (const { segment } of graphemeSegmenter.segment(value)) {
    if (segment === "\t") for (let count = 0; count < 8; count++) result.push({ text: " ", width: 1 });
    else result.push({ text: segment, width: graphemeWidth(segment) });
  }
  return result;
}

/** The cells value occupies in a terminal. */
export function cellWidth(value: string): number {
  return graphemes(value).reduce((total, part) => total + part.width, 0);
}

/** value on one line and cut to limit cells, with an ellipsis when cut. */
export function cut(value: string, limit: number): string {
  let text = value;
  for (const lineBreak of ["<br/>", "<br />", "<br>", "<BR>", "<BR/>"])
    text = text.replaceAll(lineBreak, " ");
  text = trimSpace(text.replaceAll("\t", " "));
  if (cellWidth(text) <= limit) return text;
  let kept = "";
  let width = 0;
  for (const part of graphemes(text)) {
    if (width + part.width <= limit - 1) {
      kept += part.text;
      width += part.width;
    }
  }
  return `${kept}…`;
}
