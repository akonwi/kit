// Colors for code in replies, from the server's highlighter (ADR 0062). Every
// labelled block a reply shows is asked for in one batch; a block the server
// can't color, a failed request, or colors that don't fit the text all leave
// the block plain.
import { type Accessor, createContext, createSignal, type Setter } from "solid-js";
import type { Highlight, HighlightCodeBlock, HighlightCodeResult } from "../api/types";

/** The server's bounds on one `highlightCode` request. */
export const BOUNDS = {
  blocks: 128,
  blockBytes: 512 * 1024,
  totalBytes: 768 * 1024,
  languageBytes: 64,
} as const;

/**
 * How each role is drawn, as the Mac draws code: comments muted; strings
 * and escapes green; keywords, types, functions, and builtins in the
 * accent. Any other role, including one added later, draws as text.
 */
const TONES: Record<string, "comment" | "string" | "keyword"> = {
  comment: "comment",
  string: "string",
  escape: "string",
  keyword: "keyword",
  keywordType: "keyword",
  function: "keyword",
  type: "keyword",
  builtin: "keyword",
};

/** A run of a line's text, and the tone it draws in, if any. */
export interface Run {
  text: string;
  tone?: "comment" | "string" | "keyword";
}

/** The lines the server counts: split at \n, where a final \n ends the last line. */
export function codeLines(source: string): string[] {
  if (source === "") return [];
  const lines = source.split("\n");
  if (source.endsWith("\n")) lines.pop();
  return lines;
}

/**
 * Each line of the source as runs in their tones, or undefined when the
 * colors don't describe this text: a different number of lines, or a range
 * that is out of order, out of the line, or names no role.
 */
export function coloredLines(source: string, highlight: Highlight): Run[][] | undefined {
  const lines = codeLines(source);
  if (highlight.lines.length !== lines.length) return undefined;
  const result: Run[][] = [];
  for (const [index, line] of lines.entries()) {
    const triples = highlight.lines[index] ?? [];
    if (triples.length % 3 !== 0) return undefined;
    const runs: Run[] = [];
    let at = 0;
    const push = (text: string, tone?: Run["tone"]) => {
      if (!text) return;
      const last = runs.at(-1);
      if (last && last.tone === tone) last.text += text;
      else runs.push(tone ? { text, tone } : { text });
    };
    for (let offset = 0; offset < triples.length; offset += 3) {
      const [start = -1, length = -1, role = -1] = triples.slice(offset, offset + 3);
      const valid =
        Number.isInteger(start) &&
        Number.isInteger(length) &&
        Number.isInteger(role) &&
        start >= at &&
        length >= 0 &&
        start + length <= line.length &&
        role >= 0 &&
        role < highlight.roles.length;
      if (!valid) return undefined;
      push(line.slice(at, start));
      push(line.slice(start, start + length), TONES[highlight.roles[role] ?? ""]);
      at = start + length;
    }
    push(line.slice(at));
    result.push(runs);
  }
  return result;
}

const encoder = new TextEncoder();
const bytes = (text: string) => encoder.encode(text).byteLength;

/** Whether a block may be asked for at all: a label, and within the server's bounds. */
export function askable(language: string, source: string): boolean {
  const label = language.trim();
  return (
    Boolean(label) &&
    bytes(label) <= BOUNDS.languageBytes &&
    Boolean(source) &&
    bytes(source) <= BOUNDS.blockBytes
  );
}

/** Blocks grouped into requests the server accepts, in order. */
export function batches<T extends HighlightCodeBlock>(blocks: readonly T[]): T[][] {
  const result: T[][] = [];
  let current: T[] = [];
  let total = 0;
  for (const block of blocks) {
    const size = bytes(block.source);
    if (current.length > 0 && (current.length >= BOUNDS.blocks || total + size > BOUNDS.totalBytes)) {
      result.push(current);
      current = [];
      total = 0;
    }
    current.push(block);
    total += size;
  }
  if (current.length > 0) result.push(current);
  return result;
}

export interface Highlighter {
  /** A block's colors once they arrive; undefined while waiting, or for good if it stays plain. */
  colors(language: string, source: string): Accessor<Highlight | undefined>;
}

/** The most blocks whose colors are kept. */
const KEEP = 256;

interface Entry {
  value: Accessor<Highlight | undefined>;
  set: Setter<Highlight | undefined>;
}

/**
 * A highlighter that asks for every block shown in the same moment in one
 * request, and remembers what came back, so a reply drawn again isn't asked
 * for twice.
 */
export function createHighlighter(
  send: (blocks: HighlightCodeBlock[], signal?: AbortSignal) => Promise<HighlightCodeResult>,
): Highlighter {
  const entries = new Map<string, Entry>();
  // A server without the operation stays without it for the page's life.
  let unavailable = false;
  let waiting: (HighlightCodeBlock & { key: string; entry: Entry })[] = [];
  const none: Accessor<undefined> = () => undefined;

  const flush = async () => {
    const queued = waiting;
    waiting = [];
    for (const batch of batches(queued)) {
      try {
        const result = await send(batch.map(({ language, source }) => ({ language, source })));
        const answers = Array.isArray(result?.blocks) ? result.blocks : [];
        if (answers.length !== batch.length) throw new Error("the answer has the wrong number of blocks");
        for (const [index, { entry }] of batch.entries()) entry.set(answers[index]?.highlight);
      } catch (error) {
        // Plain text is the designed fallback. A server that refused the
        // request would refuse it again, so its blocks stay plain; one that
        // couldn't answer is asked again when the blocks are next drawn.
        const status = (error as { status?: unknown } | null)?.status;
        if (status === 404 || status === 405) unavailable = true;
        if (typeof status === "number" && status >= 400 && status < 500) continue;
        for (const { key, entry } of batch) if (entries.get(key) === entry) entries.delete(key);
      }
    }
  };

  return {
    colors(language, source) {
      if (unavailable || !askable(language, source)) return none;
      const key = `${language.trim()}\u0000${source}`;
      const known = entries.get(key);
      if (known) {
        entries.delete(key);
        entries.set(key, known);
        return known.value;
      }
      const [value, set] = createSignal<Highlight | undefined>();
      const entry = { value, set };
      entries.set(key, entry);
      while (entries.size > KEEP) {
        const oldest = entries.keys().next().value;
        if (oldest === undefined) break;
        entries.delete(oldest);
      }
      if (waiting.length === 0) queueMicrotask(() => void flush());
      waiting.push({ key, entry, language: language.trim(), source });
      return value;
    },
  };
}

/** The page's highlighter; without one, code draws plain. */
export const HighlightContext = createContext<Highlighter | undefined>();
