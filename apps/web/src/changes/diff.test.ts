import { describe, expect, test } from "bun:test";
import type { DiffFileSummary, FileDiffPage } from "../api/types";
import {
  appendHunks,
  type DiffRow,
  fileCounts,
  filesLabel,
  fileTag,
  pageHunks,
  rowFor,
  type Selection,
  selected,
  selectionProblem,
  summarize,
  tapLine,
} from "./diff";

const observation = {} as FileDiffPage["observation"];

function file(value: Partial<DiffFileSummary> = {}): DiffFileSummary {
  return {
    path: "src/watch.ts",
    change: "modified",
    contentState: "text",
    old: { kind: "regular", mode: 0o100644 },
    new: { kind: "regular", mode: 0o100644 },
    fileRevision: "diff_file_A",
    ...value,
  };
}

function page(value: Partial<FileDiffPage>): FileDiffPage {
  return { observation, file: file(), computation: { state: "complete" }, hunks: [], ...value };
}

const hunk = {
  oldStart: 86,
  oldCount: 3,
  newStart: 86,
  newCount: 4,
  continuedBefore: false,
  continuedAfter: false,
  lines: [
    { kind: "context", oldLine: 86, newLine: 86, content: "let cursor;", hasTerminatingLF: true },
    { kind: "deletion", oldLine: 87, content: "connect(id, {});", hasTerminatingLF: true },
    { kind: "addition", newLine: 87, content: "connect(id, { after });", hasTerminatingLF: true },
    { kind: "addition", newLine: 88, content: "onevent = save;", hasTerminatingLF: true },
    { kind: "context", oldLine: 88, newLine: 89, content: "}", hasTerminatingLF: true },
  ],
};

const context: DiffRow = { kind: "context", oldLine: 86, newLine: 86, content: "let cursor;" };
const removed: DiffRow = { kind: "deletion", oldLine: 87, content: "connect(id, {});" };
const added: DiffRow = { kind: "addition", newLine: 87, content: "connect(id, { after });" };
const added2: DiffRow = { kind: "addition", newLine: 88, content: "onevent = save;" };
const closing: DiffRow = { kind: "context", oldLine: 88, newLine: 89, content: "}" };
const rows = [context, removed, added, added2, closing];

describe("pageHunks", () => {
  test("draws a hunk's lines with its header", () => {
    expect(pageHunks(page({ hunks: [hunk] }))).toEqual([{ header: "@@ -86,3 +86,4 @@", rows }]);
  });

  test("colors removed lines with the old side and the rest with the new", () => {
    const [view] = pageHunks(
      page({
        hunks: [hunk],
        highlight: {
          // Old side: lines 86, 87, 88 in page order; new side: 86, 87, 88, 89.
          old: { language: "typescript", roles: ["keyword"], lines: [[0, 3, 0], [], []] },
          new: { language: "typescript", roles: ["keyword"], lines: [[0, 3, 0], [], [0, 7, 0], []] },
        },
      }),
    );
    expect(view?.rows.map((row) => row.runs)).toEqual([
      [{ text: "let", tone: "keyword" }, { text: " cursor;" }],
      [{ text: "connect(id, {});" }],
      [{ text: "connect(id, { after });" }],
      [{ text: "onevent", tone: "keyword" }, { text: " = save;" }],
      [{ text: "}" }],
    ]);
  });

  test("leaves a side plain when its colors don't fit the lines", () => {
    const [view] = pageHunks(
      page({ hunks: [hunk], highlight: { new: { language: "typescript", roles: [], lines: [[]] } } }),
    );
    expect(view?.rows.every((row) => row.runs === undefined)).toBe(true);
  });

  test("joins a hunk a page break split", () => {
    const first = pageHunks(
      page({ hunks: [{ ...hunk, continuedAfter: true, lines: hunk.lines.slice(0, 2) }] }),
    );
    const joined = appendHunks(
      first,
      page({ hunks: [{ ...hunk, continuedBefore: true, lines: hunk.lines.slice(2) }] }),
    );
    expect(joined).toEqual([{ header: "@@ -86,3 +86,4 @@", rows }]);
  });
});

describe("selection", () => {
  test("a first tap picks the line on its own side", () => {
    expect(tapLine(undefined, "a.ts", added)).toEqual({ path: "a.ts", side: "new", anchor: 87, head: 87 });
    expect(tapLine(undefined, "a.ts", removed)).toEqual({ path: "a.ts", side: "old", anchor: 87, head: 87 });
  });

  test("a second tap on the same side extends to it, and context counts on either side", () => {
    const first = tapLine(undefined, "a.ts", added);
    expect(tapLine(first, "a.ts", closing)).toEqual({ path: "a.ts", side: "new", anchor: 87, head: 89 });
    const old = tapLine(undefined, "a.ts", removed);
    expect(tapLine(old, "a.ts", context)).toEqual({ path: "a.ts", side: "old", anchor: 87, head: 86 });
  });

  test("a tap on the other side or file starts over, and on the only line clears it", () => {
    const first = tapLine(undefined, "a.ts", added);
    expect(tapLine(first, "a.ts", removed)).toEqual({ path: "a.ts", side: "old", anchor: 87, head: 87 });
    expect(tapLine(first, "b.ts", added)).toEqual({ path: "b.ts", side: "new", anchor: 87, head: 87 });
    expect(tapLine(first, "a.ts", added)).toBeUndefined();
  });

  test("marks the rows on the selection's side within its range", () => {
    const selection: Selection = { path: "a.ts", side: "new", anchor: 89, head: 87 };
    expect(rows.map((row) => selected(selection, "a.ts", row))).toEqual([false, false, true, true, true]);
    expect(rows.map((row) => selected(selection, "b.ts", row))).toEqual([false, false, false, false, false]);
  });

  test("refuses a range longer than a comment covers, or one with lines not shown", () => {
    const long = Array.from(
      { length: 201 },
      (_, index): DiffRow => ({
        kind: "context",
        oldLine: index + 1,
        newLine: index + 1,
        content: "",
      }),
    );
    expect(selectionProblem({ path: "a.ts", side: "new", anchor: 1, head: 200 }, long)).toBeUndefined();
    expect(selectionProblem({ path: "a.ts", side: "new", anchor: 1, head: 201 }, long)).toBe(
      "A comment covers at most 200 lines.",
    );
    expect(selectionProblem({ path: "a.ts", side: "new", anchor: 87, head: 89 }, rows)).toBeUndefined();
    const gap = [context, added, { ...closing, oldLine: 120, newLine: 121 }];
    expect(selectionProblem({ path: "a.ts", side: "new", anchor: 87, head: 121 }, gap)).toBe(
      "Pick lines from one block of changes.",
    );
  });

  test("a note goes under the last of its lines shown", () => {
    expect(rowFor(rows, "new", 87, 88)).toBe(3);
    expect(rowFor(rows, "old", 86, 87)).toBe(1);
    expect(rowFor(rows, "new", 120, 121)).toBe(-1);
  });
});

describe("files", () => {
  test("counts a file's lines once all its hunks are read, unless the server counted them", () => {
    const views = pageHunks(page({ hunks: [hunk] }));
    expect(fileCounts(file(), views, false)).toBeUndefined();
    expect(fileCounts(file(), views, true)).toEqual({ additions: 2, deletions: 1 });
    expect(fileCounts(file({ additions: 12, deletions: 3 }), [], false)).toEqual({
      additions: 12,
      deletions: 3,
    });
  });

  test("totals lines only when the server counted every file", () => {
    expect(summarize([file({ additions: 12, deletions: 3 }), file({ additions: 30 })], false)).toEqual({
      files: 2,
      counts: { additions: 42, deletions: 3 },
      more: false,
    });
    expect(summarize([file({ additions: 12 }), file()], false)).toEqual({ files: 2, more: false });
    expect(filesLabel({ files: 1, more: false })).toBe("1 file");
    expect(filesLabel({ files: 200, more: true })).toBe("200+ files");
  });

  test("tags a file that isn't text, or that's new or gone", () => {
    expect(fileTag(file({ contentState: "binary", reason: "nul" }))).toBe("Binary");
    expect(fileTag(file({ change: "added" }))).toBe("New");
    expect(fileTag(file({ change: "deleted" }))).toBe("Deleted");
    expect(fileTag(file())).toBeUndefined();
  });
});
