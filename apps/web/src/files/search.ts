// Fuzzy search over a session's file paths, ranked as the Apple clients'
// FilePathSearch ranks them: an exact match, then a prefix, then a
// substring, then the letters in order; ties keep the paths' order.

export interface PathMatch {
  path: string;
  /** The matched characters' positions in the path, by code point. */
  matches: number[];
}

const encoder = new TextEncoder();
const BOUNDARIES = new Set([..." -_:/ .+#@\t"]);

function range(start: number, length: number): number[] {
  return Array.from({ length }, (_, index) => start + index);
}

/** A path's score for a lowercased query, and where it matched, or undefined. */
export function scorePath(path: string, query: string): { score: number; matches: number[] } | undefined {
  const candidate = path.toLowerCase();
  const text = Array.from(candidate);
  const needle = Array.from(query);
  const bytes = encoder.encode(query).byteLength;
  if (candidate === query) return { score: 1000, matches: range(0, text.length) };
  if (candidate.startsWith(query)) return { score: 900 + bytes * 4, matches: range(0, needle.length) };
  const boundary = (index: number) => index === 0 || BOUNDARIES.has(text[index - 1] ?? "");
  const found = candidate.indexOf(query);
  if (found >= 0) {
    // Positions count code points, as the path is drawn.
    const index = Array.from(candidate.slice(0, found)).length;
    return {
      score: 800 + bytes * 4 - index + (boundary(index) ? 50 : 0),
      matches: range(index, needle.length),
    };
  }
  const matches: number[] = [];
  let consecutive = 0;
  let boundaries = 0;
  for (const [index, character] of text.entries()) {
    if (character !== needle[matches.length]) continue;
    const last = matches.at(-1);
    if (last === index - 1) consecutive += 1;
    if (boundary(index)) boundaries += 1;
    matches.push(index);
    if (matches.length === needle.length) {
      const start = matches[0] ?? 0;
      const score =
        500 + needle.length * 8 + consecutive * 12 + boundaries * 10 - start * 2 - (index - start + 1);
      return { score, matches };
    }
  }
  return undefined;
}

/**
 * The paths matching `query`, best first, at most `limit` of them. An empty
 * query returns the first paths in their order.
 */
export function searchPaths(query: string, paths: readonly string[], limit = Infinity): PathMatch[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return paths.slice(0, limit).map((path) => ({ path, matches: [] }));
  const scored: { path: string; matches: number[]; score: number; index: number }[] = [];
  for (const [index, path] of paths.entries()) {
    const result = scorePath(path, needle);
    if (result) scored.push({ path, index, ...result });
  }
  scored.sort((a, b) => (a.score === b.score ? a.index - b.index : b.score - a.score));
  return scored.slice(0, limit).map(({ path, matches }) => ({ path, matches }));
}

/** A path's parts, each marked where it matched, for drawing. */
export function markedParts(
  text: string,
  matches: readonly number[],
  offset = 0,
): { text: string; hit: boolean }[] {
  const hits = new Set(matches);
  const parts: { text: string; hit: boolean }[] = [];
  for (const [index, character] of Array.from(text).entries()) {
    const hit = hits.has(index + offset);
    const last = parts.at(-1);
    if (last && last.hit === hit) last.text += character;
    else parts.push({ text: character, hit });
  }
  return parts;
}
