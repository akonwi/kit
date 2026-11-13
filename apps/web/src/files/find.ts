// Finds a plain-text query in a file's text the way every Kit client's find
// bar does. testdata/find at the repository root holds the rules and the
// cases each client's matcher agrees on; internal/textfind is the
// reference. Ranges are UTF-16 offsets, as JavaScript strings count.

/** How many matches findInText counts unless told otherwise. */
export const defaultFindLimit = 10_000;

/** The find bar's options. */
export interface FindOptions {
  /** Compare code points exactly instead of by simple case folding. */
  matchCase?: boolean;
  /** Keep only matches that don't join a word character on either side. */
  wholeWord?: boolean;
  /** How many matches to count; zero or less means defaultFindLimit. */
  limit?: number;
}

/** A match's half-open UTF-16 range. */
export interface FindRange {
  start: number;
  end: number;
}

/** What findInText found. */
export interface FindResult {
  /** In text order; they never overlap. */
  matches: FindRange[];
  /** Whether another match exists past the limit. */
  capped: boolean;
}

/**
 * The matches of query in text, scanning left to right. An empty query, or
 * one with a line feed or carriage return, matches nothing.
 */
export function findInText(text: string, query: string, options: FindOptions = {}): FindResult {
  const result: FindResult = { matches: [], capped: false };
  if (query === "" || /[\n\r]/.test(query)) return result;
  const matchCase = options.matchCase ?? false;
  const limit = options.limit && options.limit > 0 ? options.limit : defaultFindLimit;
  const needle = Array.from(query, (character) => key(character.codePointAt(0) ?? 0, matchCase));

  // The text as code points, folded unless matching case, with the UTF-16
  // offset each starts at and one past the end.
  const scalars = new Uint32Array(text.length);
  const offsets = new Uint32Array(text.length + 1);
  let count = 0;
  for (let offset = 0; offset < text.length; count++) {
    const value = text.codePointAt(offset) ?? 0;
    scalars[count] = key(value, matchCase);
    offsets[count] = offset;
    offset += value > 0xffff ? 2 : 1;
  }
  offsets[count] = text.length;

  const offsetOf = (index: number) => offsets[index] ?? text.length;
  const pointAt = (index: number) => text.codePointAt(offsetOf(index)) ?? 0;
  const isWholeWord = (start: number, end: number) =>
    !(start > 0 && joins(pointAt(start - 1), pointAt(start))) &&
    !(end < count && joins(pointAt(end - 1), pointAt(end)));

  let start = 0;
  scan: while (start + needle.length <= count) {
    for (let index = 0; index < needle.length; index++) {
      if (scalars[start + index] !== needle[index]) {
        start++;
        continue scan;
      }
    }
    const end = start + needle.length;
    if (options.wholeWord && !isWholeWord(start, end)) {
      start++;
      continue;
    }
    if (result.matches.length === limit) {
      result.capped = true;
      break;
    }
    result.matches.push({ start: offsetOf(start), end: offsetOf(end) });
    start = end;
  }
  return result;
}

/**
 * The index of the first match that starts at or after offset, the first
 * match when none does, or undefined when there are none.
 */
export function followingMatch(matches: readonly FindRange[], offset: number): number | undefined {
  if (matches.length === 0) return undefined;
  let low = 0;
  let high = matches.length;
  while (low < high) {
    const middle = (low + high) >> 1;
    if ((matches[middle]?.start ?? offset) < offset) low = middle + 1;
    else high = middle;
  }
  return low === matches.length ? 0 : low;
}

/**
 * The count a find bar shows: current is the one-based index of the current
 * match, total the matches counted, capped whether more exist, and
 * searchedLines the lines a cut-short file arrived with, or 0 when the whole
 * file arrived.
 */
export function findLabel(current: number, total: number, capped = false, searchedLines = 0): string {
  let label = total > 0 ? `${grouped(current)} of ${grouped(total)}${capped ? "+" : ""}` : "No matches";
  if (searchedLines > 0) label += ` in the first ${grouped(searchedLines)} lines`;
  return label;
}

/** Commas between groups of three digits, whatever the locale. */
function grouped(value: number): string {
  return String(value).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

function key(value: number, matchCase: boolean): number {
  return matchCase ? value : fold(value);
}

const folded = new Map<number, number>();

/**
 * One member of a code point's simple case folding orbit, the same for each
 * member. JavaScript has no simple case folding, so this folds by the
 * single-code-point case mappings and corrects the four code points where
 * that differs from Unicode's simple folding.
 */
function fold(value: number): number {
  if (value < 0x80) return value >= 0x41 && value <= 0x5a ? value + 0x20 : value;
  const cached = folded.get(value);
  if (cached !== undefined) return cached;
  const result = foldExceptions.get(value) ?? single(single(value, "toUpperCase"), "toLowerCase");
  folded.set(value, result);
  return result;
}

/**
 * ı folds to nothing else, and three pairs fold together though uppercasing
 * one of each pair gives several code points.
 */
const foldExceptions = new Map([
  [0x0131, 0x0131],
  [0x1fd3, 0x0390],
  [0x1fe3, 0x03b0],
  [0xfb05, 0xfb06],
]);

function single(value: number, mapping: "toUpperCase" | "toLowerCase"): number {
  const mapped = String.fromCodePoint(value)[mapping]();
  const first = mapped.codePointAt(0) ?? value;
  return mapped.length === (first > 0xffff ? 2 : 1) ? first : value;
}

/** Whether two adjacent code points belong to one word. */
function joins(a: number, b: number): boolean {
  return isWordCharacter(a) && isWordCharacter(b) && !isOwnWord(a) && !isOwnWord(b);
}

const wordCharacter = /^[\p{L}\p{M}\p{Nd}_]$/u;
const ideograph = /^\p{Ideographic}$/u;

function isWordCharacter(value: number): boolean {
  if (value < 0x80) {
    return (
      value === 0x5f ||
      (value >= 0x30 && value <= 0x39) ||
      (value >= 0x41 && value <= 0x5a) ||
      (value >= 0x61 && value <= 0x7a)
    );
  }
  return wordCharacter.test(String.fromCodePoint(value));
}

/** Ideographs and kana are each a word of their own. */
function isOwnWord(value: number): boolean {
  return (
    (value >= 0x3040 && value <= 0x30ff) ||
    (value >= 0x31f0 && value <= 0x31ff) ||
    (value >= 0xff66 && value <= 0xff9f) ||
    ideograph.test(String.fromCodePoint(value))
  );
}
