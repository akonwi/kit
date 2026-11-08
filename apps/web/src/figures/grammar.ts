// The grammar every figure reads (ADR 0054, docs/figures.md), ported from
// the macOS client's port of the terminal client's reference in
// `apps/cli/tui/figure/grammar.ard`. It accepts and rejects exactly what the
// reference does, with the same reasons, as the shared fixtures in
// `testdata/figures` check.
//
// The reference matches structure on UTF-8 bytes. Every separator it looks
// for is a single UTF-16 unit, so offsets here are UTF-16 indices and match
// the same places; sizes the reference limits in bytes are measured in bytes.

/** Why a figure isn't drawn. */
export class NotDrawn {
  constructor(readonly reason: string) {}
}

/** A value, or why there isn't one. */
export type Result<T> = { ok: true; value: T } | { ok: false; error: NotDrawn };

export const ok = <T>(value: T): Result<T> => ({ ok: true, value });
export const fail = <T = never>(reason: string): Result<T> => ({ ok: false, error: new NotDrawn(reason) });

/** The largest body drawn, in bytes. */
export const MAX_BODY = 8192;
/** The longest info string read, in bytes. */
export const MAX_INFO = 1024;
/** The most attributes an info string has. */
export const MAX_ATTRIBUTES = 8;
/** The largest magnitude a number may have. */
export const MAX_NUMBER = 1e18;
/** The most decimals a computed number is written with. */
export const MAX_DECIMALS = 6;

export interface Attribute {
  key: string;
  value: string;
}

/** A fence's info string, split into its name and attributes. */
export interface Info {
  name: string;
  attributes: Attribute[];
}

/** The value of attribute key, or empty. */
export function attribute(info: Info, key: string): string {
  for (let index = info.attributes.length - 1; index >= 0; index--) {
    const each = info.attributes[index];
    if (each?.key === key) return each.value;
  }
  return "";
}

/** One list item of a body: its text without its marker, emphasis, or note. */
export interface Item {
  text: string;
  note: string;
  bold: boolean;
  italic: boolean;
  details: string[];
  /** The indent of each of details, in spaces, where a tab counts as two. */
  depths: number[];
  /** The number an ordered item is written with, or -1 for a `- ` item. */
  ordinal: number;
}

// Go string semantics.

const LEADING_SPACE = /^\p{White_Space}+/u;
const TRAILING_SPACE = /\p{White_Space}+$/u;

/** Go's `strings.TrimSpace`. */
export function trimSpace(value: string): string {
  return value.replace(LEADING_SPACE, "").replace(TRAILING_SPACE, "");
}

/** Go's `strings.Fields`. */
export function fields(value: string): string[] {
  return value.split(/\p{White_Space}+/u).filter((each) => each !== "");
}

function trimLeft(value: string, set: string): string {
  let start = 0;
  while (start < value.length && set.includes(value[start] ?? "")) start++;
  return value.slice(start);
}

function trimRight(value: string, set: string): string {
  let end = value.length;
  while (end > 0 && set.includes(value[end - 1] ?? "")) end--;
  return value.slice(0, end);
}

const encoder = new TextEncoder();

/** text's length in UTF-8 bytes. */
export function byteLength(text: string): number {
  return encoder.encode(text).length;
}

/** The offset of separator's first occurrence in text, or undefined. */
export function index(text: string, separator: string): number | undefined {
  const at = text.indexOf(separator);
  return at < 0 ? undefined : at;
}

// Info strings.

/** The first word of an info string. */
export function name(info: string): string {
  const trimmed = trimSpace(info);
  const space = trimmed.search(/[ \t]/);
  return space < 0 ? trimmed : trimmed.slice(0, space);
}

/** An info string's name and attributes, each key at most once. */
export function parseInfo(info: string): Result<Info> {
  const trimmed = trimSpace(info);
  if (byteLength(trimmed) > MAX_INFO) return fail(`the fence's info string is over ${MAX_INFO} bytes`);
  const figure = name(trimmed);
  let rest = trimLeft(trimmed.slice(figure.length), " \t");
  const attributes: Attribute[] = [];
  let repeated = "";
  while (rest !== "") {
    const key = /^[a-z][a-z0-9-]*/.exec(rest)?.[0] ?? "";
    const after = rest.slice(key.length);
    if (key === "" || !after.startsWith('="'))
      return fail(`the attributes aren't \`key="value"\` pairs near \`${clip(rest)}\``);
    const quoted = after.slice(2);
    const close = index(quoted, '"');
    if (close === undefined) return fail(`the value of \`${key}\` isn't closed`);
    const next = quoted.slice(close + 1);
    if (next !== "" && !next.startsWith(" ") && !next.startsWith("\t"))
      return fail(`the attributes aren't \`key="value"\` pairs near \`${clip(next)}\``);
    if (repeated === "" && attributes.some((each) => each.key === key)) repeated = key;
    attributes.push({ key, value: clean(quoted.slice(0, close)) });
    rest = trimLeft(next, " \t");
  }
  if (attributes.length > MAX_ATTRIBUTES) return fail(`a figure has at most ${MAX_ATTRIBUTES} attributes`);
  if (repeated !== "") return fail(`\`${repeated}\` is set more than once`);
  return ok({ name: figure, attributes });
}

/** Rejects an attribute that isn't one of allowed. */
export function checkAttributes(info: Info, allowed: string[]): NotDrawn | undefined {
  const unknown = info.attributes.find((each) => !allowed.includes(each.key));
  return unknown ? new NotDrawn(`\`${info.name}\` has no \`${unknown.key}\` attribute`) : undefined;
}

// Text.

/** Text shown in an error: its first 24 bytes, less a character cut in two. */
export function clip(text: string): string {
  if (byteLength(text) <= 24) return text;
  let kept = "";
  let size = 0;
  for (const point of text) {
    const width = byteLength(point);
    if (size + width > 24) break;
    kept += point;
    size += width;
  }
  return `${kept}…`;
}

/** text without control characters; a tab is a space. */
export function clean(text: string): string {
  let result = "";
  for (const point of text) {
    if (point === "\t") {
      result += " ";
      continue;
    }
    const value = point.codePointAt(0) ?? 0;
    if (value <= 0x1f || (value >= 0x7f && value <= 0x9f)) continue;
    result += point;
  }
  return result;
}

/** The leading spaces of line, where a tab counts as two. */
function indent(line: string): number {
  let count = 0;
  for (const character of line) {
    if (character === " ") count += 1;
    else if (character === "\t") count += 2;
    else break;
  }
  return count;
}

/** An item's text split into its body and the note after ` — ` or ` -- `. */
function splitNote(text: string): [string, string] {
  const dash = index(text, " — ");
  const found = dash !== undefined ? [dash, 3] : [index(text, " -- "), 4];
  const [at, width] = found;
  if (at === undefined || width === undefined) return [trimSpace(text), ""];
  return [trimSpace(text.slice(0, at)), trimSpace(text.slice(at + width))];
}

/** Whether text is wrapped whole in marker, with something inside. */
function wrapped(text: string, marker: string): boolean {
  return text.length > marker.length * 2 && text.startsWith(marker) && text.endsWith(marker);
}

function emphasized(body: string, note: string): Item {
  const bold = wrapped(body, "**");
  const italic = !bold && wrapped(body, "*") && !body.startsWith("**");
  const inner = bold ? body.slice(2, -2) : italic ? body.slice(1, -1) : body;
  return { text: trimSpace(inner), note, bold, italic, details: [], depths: [], ordinal: -1 };
}

/** One line read as an item is: its emphasis and note split off. */
export function line(text: string): Item {
  const [body, note] = splitNote(clean(text));
  return emphasized(body, note);
}

/** text with its emphasis split off but no note, as a table cell is read. */
export function emphasis(text: string): Item {
  return emphasized(trimSpace(clean(text)), "");
}

// `.` matches line separators such as U+2028, as it does in Go.
const ORDERED = /^([0-9]{1,9})\. (.*)$/s;

/** A trimmed line's list marker and text: `- text` or `N. text`. */
function marked(trimmed: string): { ordinal: number; text: string } | undefined {
  if (trimmed.startsWith("- ")) return { ordinal: -1, text: trimmed.slice(2) };
  const match = ORDERED.exec(trimmed);
  if (!match) return undefined;
  return { ordinal: Number(match[1]), text: match[2] ?? "" };
}

/**
 * A trimmed line read as a list item, `- text` or `N. text`, with its
 * emphasis and note split off; or undefined when it isn't one.
 */
export function listItem(trimmed: string): Item | undefined {
  const marker = marked(trimmed);
  if (!marker) return undefined;
  return { ...line(marker.text), ordinal: marker.ordinal };
}

/** Whether a trimmed line starts an ordered item, as in `1. text`. */
export function isOrdered(trimmed: string): boolean {
  return (marked(trimmed)?.ordinal ?? -1) >= 0;
}

/** text's lines, a `\r\n` read as one break. */
export function lines(text: string): string[] {
  return text.replace(/\r\n/g, "\n").split("\n");
}

/**
 * A body's `- ` or `N. ` list items, each with the indented lines under it.
 * Blank lines are skipped; any other line is an error.
 */
export function items(body: string): Result<Item[]> {
  const listed = list(body, false);
  return listed.ok ? ok(listed.value.items) : listed;
}

/** A body's list items and the one paragraph after them, its lines joined, or empty. */
export function itemsAndNote(body: string): Result<{ items: Item[]; note: string }> {
  return list(body, true);
}

function list(body: string, closing: boolean): Result<{ items: Item[]; note: string }> {
  if (byteLength(body) > MAX_BODY) return fail(`the figure is larger than ${MAX_BODY / 1024} KiB`);
  const result: Item[] = [];
  let current: Item | undefined;
  const note: string[] = [];
  // Whether the line before was blank, once the closing paragraph started.
  let afterBlank = false;
  for (const raw of lines(body)) {
    const text = trimRight(raw, " \t\r");
    const trimmed = trimSpace(text);
    const marker = indent(text) < 2 ? marked(trimmed) : undefined;
    const inNote = note.length > 0;
    if (trimmed === "") {
      afterBlank = inNote;
    } else if (inNote && afterBlank) {
      return fail("the figure takes one paragraph after its list");
    } else if (inNote && marker) {
      return fail(`the paragraph comes after every item, got \`${clip(trimmed)}\``);
    } else if (inNote) {
      note.push(clean(trimmed));
    } else if (marker) {
      if (current) result.push(current);
      current = { ...line(marker.text), ordinal: marker.ordinal };
    } else if (indent(text) >= 2 && current) {
      current.details.push(clean(trimmed));
      current.depths.push(indent(text));
    } else if (closing && current) {
      note.push(clean(trimmed));
    } else {
      return fail(`expected a \`- \` or \`1. \` list item, got \`${clip(trimmed)}\``);
    }
  }
  if (current) result.push(current);
  return ok({ items: result, note: note.join(" ") });
}

/** Rejects more than one bold item. */
export function checkOneBold(items: Item[]): NotDrawn | undefined {
  return items.filter((each) => each.bold).length > 1 ? new NotDrawn("only one item can be bold") : undefined;
}

/** text split at its first `: ` into a label and a value. */
export function labelValue(text: string): [string, string] | undefined {
  const at = index(text, ": ");
  if (at === undefined || at === 0) return undefined;
  return [trimSpace(text.slice(0, at)), trimSpace(text.slice(at + 2))];
}

// Numbers.

const NUMBER = /^([+-]?)\$?([0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)(\.[0-9]+)? ?(?:[A-Za-z]{1,4}|%)?$/;

/**
 * A number as written: an optional sign and `$`, digits with optional `,`
 * groups and `.` fraction, and an optional unit such as `%`, `ms`, or `kb`.
 */
export function number(text: string): Result<number> {
  const match = NUMBER.exec(text);
  if (!match) return fail(`\`${clip(text)}\` isn't a number`);
  const value = Number((match[1] ?? "") + (match[2] ?? "").replace(/,/g, "") + (match[3] ?? ""));
  if (!Number.isFinite(value)) return fail(`\`${clip(text)}\` isn't a number`);
  if (Math.abs(value) > MAX_NUMBER) return fail(`\`${clip(text)}\` is too large`);
  return ok(value);
}

/** How a number is written, so a number computed from it is written alike. */
export interface Shape {
  dollar: boolean;
  /** Whether its digits are grouped with `,`. */
  grouped: boolean;
  decimals: number;
  /** Its unit, with the space before it if there is one. */
  unit: string;
}

const SHAPE = /^[+-]?(\$?)([0-9,]+)(?:\.([0-9]+))?( ?(?:[A-Za-z]{1,4}|%)?)$/;

/** The shape of a number as written; text is one `number` reads. */
export function shape(text: string): Shape {
  const match = SHAPE.exec(text);
  if (!match) return { dollar: false, grouped: false, decimals: 0, unit: "" };
  return {
    dollar: match[1] === "$",
    grouped: (match[2] ?? "").includes(","),
    decimals: (match[3] ?? "").length,
    unit: match[4] ?? "",
  };
}

/**
 * The shape numbers written in each of shapes share: the most decimals,
 * grouped if any is, and the first one's `$` and unit.
 */
export function common(shapes: Shape[]): Shape {
  const result: Shape = { ...(shapes[0] ?? { dollar: false, grouped: false, decimals: 0, unit: "" }) };
  for (const each of shapes) {
    result.decimals = Math.max(result.decimals, each.decimals);
    if (each.grouped) result.grouped = true;
  }
  return result;
}

/** Whether numbers of shapes a and b are in the same unit, `$` included. */
export function sameUnit(a: Shape, b: Shape): boolean {
  return a.dollar === b.dollar && trimSpace(a.unit) === trimSpace(b.unit);
}

/**
 * A non-negative value with decimals digits after the point, rounded as
 * `printf("%.*f")` rounds: from the double's exact binary value to nearest,
 * and an exact tie to even. Multiplying by a power of ten first would round
 * twice, so the digits are worked out in integers.
 */
export function fixed(value: number, decimals: number): string {
  const view = new DataView(new ArrayBuffer(8));
  view.setFloat64(0, value);
  const bits = view.getBigUint64(0);
  const exponent = Number((bits >> 52n) & 0x7ffn);
  const fraction = bits & 0xfffffffffffffn;
  // value is mantissa × 2^power exactly.
  const mantissa = exponent === 0 ? fraction : fraction | (1n << 52n);
  const power = (exponent === 0 ? 1 : exponent) - 1075;
  const scaled = mantissa * 10n ** BigInt(decimals);
  let digits: bigint;
  if (power >= 0) {
    digits = scaled << BigInt(power);
  } else {
    const divisor = 1n << BigInt(-power);
    digits = scaled / divisor;
    const twice = (scaled % divisor) * 2n;
    if (twice > divisor || (twice === divisor && digits % 2n === 1n)) digits += 1n;
  }
  const text = digits.toString().padStart(decimals + 1, "0");
  return decimals === 0 ? text : `${text.slice(0, -decimals)}.${text.slice(-decimals)}`;
}

/**
 * value written in shape, with at most MAX_DECIMALS. When signed, a value
 * that isn't zero as written takes `+` or `−`.
 */
export function written(value: number, form: Shape, signed: boolean): string {
  const digits = fixed(Math.abs(value), Math.min(form.decimals, MAX_DECIMALS));
  const point = digits.indexOf(".");
  const whole = point < 0 ? digits : digits.slice(0, point);
  const fraction = point < 0 ? "" : digits.slice(point);
  const zero = /^[0.]*$/.test(digits);
  const sign = !signed || zero ? "" : value > 0 ? "+" : "−";
  const grouped = form.grouped ? whole.replace(/\B(?=(\d{3})+$)/g, ",") : whole;
  return sign + (form.dollar ? "$" : "") + grouped + fraction + form.unit;
}

/** A list of numbers separated by spaces, where `x*n` repeats x n times. */
export function numbers(text: string, limit: number): Result<number[]> {
  const result: number[] = [];
  for (const token of fields(text)) {
    const star = token.lastIndexOf("*");
    let source = token;
    let count = 1;
    if (star > 0) {
      source = token.slice(0, star);
      const run = token.slice(star + 1);
      count = /^[+-]?[0-9]+$/.test(run) ? Number(run) : -1;
    }
    if (!(count >= 1 && count <= 200)) return fail(`\`${clip(token)}\` isn't a run of 1 to 200 values`);
    const parsed = number(source);
    if (!parsed.ok) return parsed;
    for (let each = 0; each < count && result.length <= limit; each++) result.push(parsed.value);
  }
  if (result.length > limit) return fail(`there are more than ${limit} values`);
  return ok(result);
}

/** value as a share of whole, in thousandths from 0 to 1000. */
export function permille(value: number, whole: number): number {
  if (!(whole > 0)) return 0;
  const share = Math.floor((value / whole) * 1000);
  return Math.min(Math.max(share, 0), 1000);
}

/** value rounded half away from zero, as Swift's `rounded()` and Go's `math.Round`. */
export function rounded(value: number): number {
  return Math.sign(value) * Math.round(Math.abs(value));
}
