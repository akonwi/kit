// Colors for a file's lines, from Pierre's highlighter (@pierre/highlights),
// which runs in the browser as WebAssembly (ADR 0074). It loads the first
// time a file needs it. Each token kind maps to one of the tones the Mac
// draws code in, through classes, so no color arrives as an inline style.
import type { LiveTokenizer } from "@pierre/highlights";
import type { Run } from "../markdown/highlight";

type Highlights = typeof import("@pierre/highlights");
type Tone = NonNullable<Run["tone"]>;

/** Lines longer than this show plain, as drawing their tokens costs more than it helps. */
export const MAX_COLORED_LINE = 4096;

/** The highlighter's own colors go unused, so its theme has none. */
const THEME = { name: "kit", appearance: "light", style: {} } as const;

/** Names the highlighter has no language for, by the language they are. */
const NAMES: Record<string, string> = {
  gnumakefile: "makefile",
  "cmakelists.txt": "cmake",
  pyi: "python",
  plist: "xml",
};

/** Languages that draw nothing but text. */
const PLAIN = new Set(["plain", "plaintext", "text", "txt", "ansi"]);

/**
 * The highlighter's language for a path: its extension, its whole name, or a
 * name it knows another way, or undefined when it has none.
 */
export function fileLanguage(path: string, supported: (name: string) => boolean): string | undefined {
  const name = path.slice(path.lastIndexOf("/") + 1).toLowerCase();
  const dot = name.lastIndexOf(".");
  const extension = dot > 0 ? name.slice(dot + 1) : "";
  for (const candidate of [NAMES[name], extension && NAMES[extension], extension, name]) {
    if (candidate && !PLAIN.has(candidate) && supported(candidate)) return candidate;
  }
  return undefined;
}

/**
 * The tone a token kind draws in, as the Mac draws code: comments muted;
 * strings and escapes green; keywords, types, functions, builtins, and tags
 * in the accent. Any other kind draws as text.
 */
export function toneOf(token: string): Tone | undefined {
  if (token === "constant.builtin" || token === "boolean") return "keyword";
  switch (token.split(".")[0]) {
    case "comment":
      return "comment";
    case "string":
      return "string";
    case "keyword":
    case "type":
    case "function":
    case "constructor":
    case "tag":
      return "keyword";
    default:
      return undefined;
  }
}

/**
 * A line's runs from its token records, each a token's end, as a UTF-16
 * offset into the line, and its kind. Each token starts where the one
 * before it ended. Text past the last record draws as text.
 */
export function recordRuns(
  text: string,
  records: Iterable<{ end: number; token: number }>,
  tones: readonly (Tone | undefined)[],
): Run[] {
  const runs: Run[] = [];
  const push = (part: string, tone: Tone | undefined) => {
    if (!part) return;
    const last = runs.at(-1);
    if (last && last.tone === tone) last.text += part;
    else runs.push(tone ? { text: part, tone } : { text: part });
  };
  let at = 0;
  for (const { end, token } of records) {
    const stop = Math.min(end, text.length);
    if (stop <= at) continue;
    push(text.slice(at, stop), tones[token]);
    at = stop;
  }
  push(text.slice(at), undefined);
  return runs;
}

/** A file's colors, line by line, kept until disposed. */
export interface FileColors {
  /** The runs of the line at index, whose text is text, or undefined to show it plain. */
  runs(index: number, text: string): Run[] | undefined;
  dispose(): void;
}

let highlights: Promise<Highlights | undefined> | undefined;

/** The highlighter, loaded once, or undefined when it can't load here. */
function loadHighlights(): Promise<Highlights | undefined> {
  highlights ??= import("@pierre/highlights").catch(() => undefined);
  return highlights;
}

/**
 * Colors for text, the content of path split into lines, or undefined when
 * its language has no highlighter or the highlighter's lines don't match.
 */
export async function colorFile(path: string, text: string, lines: number): Promise<FileColors | undefined> {
  const module = await loadHighlights();
  if (!module) return undefined;
  const lang = fileLanguage(path, module.isSupportedLanguage);
  if (!lang) return undefined;
  let live: LiveTokenizer;
  try {
    live = new module.LiveTokenizer({ lang: lang as never, code: text, theme: THEME });
  } catch {
    return undefined;
  }
  // The highlighter counts a line after a final line end, and breaks lines
  // at a lone \r, which the view doesn't; a file whose lines differ is plain.
  const expected = text === "" || text.endsWith("\n") ? lines + 1 : lines;
  if (live.lineCount !== expected) {
    live.dispose();
    return undefined;
  }
  const tones = module.tokenNames.map(toneOf);
  let disposed = false;
  return {
    runs(index, line) {
      if (disposed || line.length > MAX_COLORED_LINE || index >= live.lineCount) return undefined;
      if (live.getLineLength(index) !== line.length) return undefined;
      return recordRuns(line, lineRecords(live.getLineRecords(index)), tones);
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      live.dispose();
    },
  };
}

/** A line's records as ends and token kinds, from either of the highlighter's layouts. */
function* lineRecords(records: { format: "packed24" | "wide32"; data: Uint32Array }) {
  const { data } = records;
  if (records.format === "packed24") {
    for (const word of data) yield { end: word & 0xffffff, token: word >>> 24 };
    return;
  }
  for (let at = 0; at + 1 < data.length; at += 2) yield { end: data[at] ?? 0, token: data[at + 1] ?? 0 };
}
