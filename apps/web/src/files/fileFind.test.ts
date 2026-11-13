import { expect, test } from "bun:test";
import { FileFind, type FindKey, fieldFindAction, pageFindAction, searchPieces } from "./fileFind";

// Two lines of a file, as the view gives them: one piece a line.
const lines = ["let cursor = start;", "cursor = next(cursor); Cursor", "done(cursorless);"];

function opened(pieces: readonly string[] = lines, query = "cursor"): FileFind {
  const find = new FileFind();
  find.setPieces(pieces, "source");
  find.open();
  find.setQuery(query);
  find.refresh();
  return find;
}

const at = (find: FileFind) =>
  find.currentMatch && { piece: find.currentMatch.piece, start: find.currentMatch.start };

test("a search finds every match in every piece and counts them", () => {
  const find = opened();
  expect(find.matches).toEqual([
    { piece: 0, start: 4, end: 10 },
    { piece: 1, start: 0, end: 6 },
    { piece: 1, start: 14, end: 20 },
    { piece: 1, start: 23, end: 29 },
    { piece: 2, start: 5, end: 11 },
  ]);
  expect(find.count).toBe("1 of 5");
  expect(find.revealRequest).toBe(1);
});

test("Match Case keeps only the exact case", () => {
  const find = opened();
  find.setMatchCase(true);
  find.refresh();
  expect(find.matches.map((match) => match.piece)).toEqual([0, 1, 1, 2]);
  find.setQuery("Cursor");
  find.refresh();
  expect(find.matches).toEqual([{ piece: 1, start: 23, end: 29 }]);
  expect(find.count).toBe("1 of 1");
});

test("Whole Word skips matches inside longer words", () => {
  const find = opened();
  find.setWholeWord(true);
  find.refresh();
  expect(find.matches.map((match) => match.start)).toEqual([4, 0, 14, 23]);
  expect(find.count).toBe("1 of 4");
});

test("next and previous wrap, asking the view to show each", () => {
  const find = opened();
  find.previous();
  expect(at(find)).toEqual({ piece: 2, start: 5 });
  expect(find.count).toBe("5 of 5");
  find.next();
  expect(at(find)).toEqual({ piece: 0, start: 4 });
  find.next();
  expect(find.count).toBe("2 of 5");
  expect(find.revealRequest).toBe(4);
});

test("a search starts at the first match from the top of the view", () => {
  const find = new FileFind();
  find.visibleAnchor = () => ({ piece: 1, offset: 3 });
  find.setPieces(lines, "source");
  find.open();
  find.setQuery("cursor");
  find.refresh();
  expect(at(find)).toEqual({ piece: 1, start: 14 });
  expect(find.count).toBe("3 of 5");

  // Past the last match, it starts again from the first.
  find.visibleAnchor = () => ({ piece: 2, offset: 6 });
  find.setQuery("");
  find.setQuery("cursor");
  find.refresh();
  expect(find.count).toBe("1 of 5");
});

test("typing more keeps the current match where it is", () => {
  const find = opened();
  find.next();
  find.next();
  expect(at(find)).toEqual({ piece: 1, start: 14 });
  find.setQuery("curso");
  find.refresh();
  find.visibleAnchor = () => ({ piece: 0, offset: 0 });
  find.setQuery("cursor");
  find.refresh();
  expect(at(find)).toEqual({ piece: 1, start: 14 });
  expect(find.count).toBe("3 of 5");
});

test("no matches say so, with nothing current to step to", () => {
  const find = opened(lines, "cursors");
  find.setQuery("cursorz");
  expect(find.count).toBe("");
  find.refresh();
  expect(find.count).toBe("No matches");
  expect(find.currentMatch).toBeUndefined();
  find.next();
  expect(find.currentMatch).toBeUndefined();
  expect(find.revealRequest).toBe(0);
  find.setQuery("");
  expect(find.count).toBe("");
});

test("a file that arrived cut short says how far it was searched", () => {
  const find = opened();
  find.searchedLines = 5000;
  expect(find.count).toBe("1 of 5");
  expect(find.note).toBe("In the first 5,000 lines");
  find.setQuery("");
  expect(find.note).toBe("");
});

test("closing gives the keyboard back, and Find Next resumes after the last match", () => {
  const find = opened();
  find.next();
  find.close();
  expect(find.isOpen).toBe(false);
  expect(find.dismissRequest).toBe(1);
  expect(find.currentMatch).toBeUndefined();
  expect(find.query).toBe("cursor");

  find.visibleAnchor = () => ({ piece: 0, offset: 0 });
  find.next();
  expect(find.isOpen).toBe(true);
  expect(find.count).toBe("");
  find.refresh();
  expect(at(find)).toEqual({ piece: 1, start: 14 });
});

test("reopening searches from the top of the view, its query selected", () => {
  const find = opened();
  find.next();
  find.close();
  find.visibleAnchor = () => ({ piece: 1, offset: 20 });
  find.open();
  expect(find.focusRequest).toBe(2);
  expect(find.stale).toBe(true);
  find.refresh();
  expect(at(find)).toEqual({ piece: 1, start: 23 });
});

test("Find Next with nothing typed opens the bar", () => {
  const find = new FileFind();
  find.setPieces(lines, "source");
  find.next();
  expect(find.isOpen).toBe(true);
  expect(find.focusRequest).toBe(1);
});

test("Use Selection for Find searches its first line from where it is", () => {
  const find = opened(lines, "done");
  find.selectedText = () => ({ text: "\ncursor = next\nmore", start: { piece: 1, offset: 14 } });
  find.useSelection();
  expect(find.query).toBe("cursor = next");
  expect(find.focusRequest).toBe(2);
  find.refresh();
  expect(at(find)).toEqual({ piece: 1, start: 0 });

  // With the bar closed, Find Next opens it at the match after the selection.
  find.close();
  find.selectedText = () => ({ text: "cursor", start: { piece: 1, offset: 14 } });
  find.useSelection();
  expect(find.isOpen).toBe(false);
  find.next();
  find.refresh();
  expect(at(find)).toEqual({ piece: 1, start: 23 });
});

test("showing something else forgets the current match", () => {
  const find = opened();
  find.next();
  find.next();
  find.visibleAnchor = () => ({ piece: 1, offset: 0 });
  find.setPieces(["the cursor", "a cursor"], "rendered");
  find.refresh();
  expect(at(find)).toEqual({ piece: 1, start: 2 });
  expect(find.count).toBe("2 of 2");
});

test("pieces are searched apart, under one limit", () => {
  expect(searchPieces(["ab", "c"], "bc").matches).toEqual([]);
  expect(searchPieces(["aa", "a", "aaa"], "a", { limit: 4 })).toEqual({
    matches: [
      { piece: 0, start: 0, end: 1 },
      { piece: 0, start: 1, end: 2 },
      { piece: 1, start: 0, end: 1 },
      { piece: 2, start: 0, end: 1 },
    ],
    capped: true,
  });
  // A limit that runs out at a piece's end looks past it for one more.
  expect(searchPieces(["aa", "b", "a"], "a", { limit: 2 }).capped).toBe(true);
  expect(searchPieces(["aa", "b", "c"], "a", { limit: 2 }).capped).toBe(false);
});

test("a large file counts up to 10,000 matches quickly", () => {
  const source = Array.from({ length: 5000 }, (_, line) => `handler ${line + 1} handler`);
  const find = new FileFind();
  find.setPieces(source, "source");
  find.open();
  find.setQuery("handler");
  const started = performance.now();
  find.refresh();
  expect(performance.now() - started).toBeLessThan(1000);
  expect(find.count).toBe("1 of 10,000");

  find.setPieces([...source, "handler"], "source");
  find.refresh();
  expect(find.count).toBe("1 of 10,000+");
});

const key = (key: string, mods: Partial<FindKey> = {}): FindKey => ({
  key,
  metaKey: false,
  ctrlKey: false,
  shiftKey: false,
  altKey: false,
  ...mods,
});

test("⌘F opens the bar, and again in its field is the browser's", () => {
  expect(pageFindAction(key("f", { metaKey: true }), true, false)).toBe("open");
  expect(pageFindAction(key("f", { metaKey: true }), true, true)).toBeUndefined();
  expect(pageFindAction(key("f", { ctrlKey: true }), false, false)).toBe("open");
  // Ctrl+F on a Mac moves the cursor in a field, so it stays the system's.
  expect(pageFindAction(key("f", { ctrlKey: true }), true, false)).toBeUndefined();
  expect(pageFindAction(key("f"), true, false)).toBeUndefined();
});

test("⌘G and ⇧⌘G step, and ⌘E finds the selection, in the field or not", () => {
  expect(pageFindAction(key("g", { metaKey: true }), true, true)).toBe("next");
  expect(pageFindAction(key("G", { metaKey: true, shiftKey: true }), true, false)).toBe("previous");
  expect(pageFindAction(key("g", { ctrlKey: true }), false, false)).toBe("next");
  expect(pageFindAction(key("e", { metaKey: true }), true, false)).toBe("useSelection");
  expect(pageFindAction(key("g", { metaKey: true, altKey: true }), true, false)).toBeUndefined();
});

test("Return steps on, Shift-Return back, and Esc closes from the field", () => {
  expect(fieldFindAction(key("Enter"))).toBe("next");
  expect(fieldFindAction(key("Enter", { shiftKey: true }))).toBe("previous");
  expect(fieldFindAction(key("Escape"))).toBe("close");
  expect(fieldFindAction(key("Enter", { metaKey: true }))).toBeUndefined();
  expect(fieldFindAction(key("a"))).toBeUndefined();
});
