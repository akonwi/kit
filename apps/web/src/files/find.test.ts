import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { defaultFindLimit, type FindRange, findInText, findLabel, followingMatch } from "./find";

interface Case {
  name: string;
  query: string;
  matchCase?: boolean;
  wholeWord?: boolean;
  limit?: number;
  capped?: boolean;
  current?: number;
  marked: string;
}

interface Label {
  current: number;
  total: number;
  capped?: boolean;
  searchedLines?: number;
  label: string;
}

// The cases every client's matcher reads.
const fixture = join(import.meta.dir, "../../../../testdata/find/cases.json");
const { cases, labels } = JSON.parse(readFileSync(fixture, "utf8")) as { cases: Case[]; labels: Label[] };

// The text of marked without its markers, the UTF-16 ranges between ⟦ and
// ⟧, and the UTF-16 offset of ‸.
function unmark(marked: string): { text: string; ranges: FindRange[]; anchor: number } {
  let text = "";
  const ranges: FindRange[] = [];
  let anchor = 0;
  for (const character of marked) {
    if (character === "⟦") ranges.push({ start: text.length, end: text.length });
    else if (character === "⟧") {
      const range = ranges.at(-1);
      if (range) range.end = text.length;
    } else if (character === "‸") anchor = text.length;
    else text += character;
  }
  return { text, ranges, anchor };
}

describe("findInText", () => {
  test("the fixture is present", () => {
    expect(cases.length).toBeGreaterThanOrEqual(50);
  });

  test.each(cases.map((c) => [c.name, c] as const))("%s", (_, c) => {
    const { text, ranges, anchor } = unmark(c.marked);
    const result = findInText(text, c.query, {
      matchCase: c.matchCase,
      wholeWord: c.wholeWord,
      limit: c.limit,
    });
    expect(result).toEqual({ matches: ranges, capped: c.capped ?? false });
    if (c.current !== undefined) expect(followingMatch(result.matches, anchor)).toBe(c.current);
  });

  test("counts ten thousand matches by default", () => {
    const exactly = "ab ".repeat(defaultFindLimit);
    const all = findInText(exactly, "ab");
    expect(all.matches.length).toBe(defaultFindLimit);
    expect(all.matches.at(-1)).toEqual({
      start: 3 * (defaultFindLimit - 1),
      end: 3 * (defaultFindLimit - 1) + 2,
    });
    expect(all.capped).toBe(false);
    const more = findInText(`${exactly}ab`, "ab");
    expect(more.matches.length).toBe(defaultFindLimit);
    expect(more.capped).toBe(true);
  });

  // A file at the most a client receives, 5,000 lines of about 200
  // characters, searches in well under the time between keystrokes, whether
  // the search reads it all or stops at the limit.
  test("searches a large file quickly", () => {
    const line = "func (s *Server) handler(next http.Handler) http.Handler { return next } ".repeat(3);
    const text = `${line}\n`.repeat(5_000);
    const began = performance.now();
    const none = findInText(text, "handlers", { wholeWord: true });
    const many = findInText(text, "e");
    const elapsed = performance.now() - began;
    expect(none.matches).toEqual([]);
    expect(many.matches.length).toBe(defaultFindLimit);
    expect(many.capped).toBe(true);
    expect(elapsed).toBeLessThan(1_000);
  });
});

test("followingMatch is undefined without matches", () => {
  expect(followingMatch([], 0)).toBeUndefined();
});

test.each(labels.map((l) => [l.label, l] as const))("findLabel reads %s", (label, l) => {
  expect(findLabel(l.current, l.total, l.capped, l.searchedLines)).toBe(label);
});
