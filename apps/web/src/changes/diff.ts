// A file's changes as rows to draw, and the lines a reader picks to comment
// on. Unified: each row has one number, the old one for a removed line and
// the new one otherwise.
import type { DiffFileSummary, DiffHunk, FileDiffPage } from "../api/types";
import { coloredLineList, type Run } from "../markdown/highlight";
import { MAX_COMMENT_LINES } from "./comments";

export type Side = "old" | "new";

export interface DiffRow {
  kind: "context" | "addition" | "deletion";
  oldLine?: number;
  newLine?: number;
  content: string;
  /** The line's text in its colors, when the server colored it. */
  runs?: Run[];
}

export interface HunkView {
  header: string;
  rows: DiffRow[];
}

/** The side a row's own number is on: removed lines are old, the rest new. */
export function sideOf(row: DiffRow): Side {
  return row.kind === "deletion" ? "old" : "new";
}

export function lineOn(row: DiffRow, side: Side): number | undefined {
  return side === "old" ? row.oldLine : row.newLine;
}

/** The number a row shows. */
export function shownLine(row: DiffRow): number | undefined {
  return lineOn(row, sideOf(row));
}

function count(start: number, length: number): string {
  return length === 1 ? `${start}` : `${start},${length}`;
}

export function hunkHeader(hunk: DiffHunk): string {
  return `@@ -${count(hunk.oldStart, hunk.oldCount)} +${count(hunk.newStart, hunk.newCount)} @@`;
}

/**
 * A page's hunks as rows, in the server's colors where they fit. Each side's
 * colors cover the page's lines that have that side, in page order; a
 * removed line takes the old side's and every other line the new side's.
 */
export function pageHunks(page: FileDiffPage): HunkView[] {
  const lines = page.hunks.flatMap((hunk) => hunk.lines);
  const colors = (side: Side) => {
    const highlight = page.highlight?.[side];
    if (!highlight) return undefined;
    const content = lines.filter((line) => (side === "old" ? line.oldLine : line.newLine) !== undefined);
    return coloredLineList(
      content.map((line) => line.content.replace(/\r$/, "")),
      highlight,
    );
  };
  const old = colors("old");
  const fresh = colors("new");
  let oldAt = 0;
  let newAt = 0;
  return page.hunks.map((hunk) => ({
    header: hunkHeader(hunk),
    rows: hunk.lines.map((line) => {
      const kind = line.kind as DiffRow["kind"];
      const oldRuns = line.oldLine !== undefined ? old?.[oldAt++] : undefined;
      const newRuns = line.newLine !== undefined ? fresh?.[newAt++] : undefined;
      const runs = kind === "deletion" ? oldRuns : newRuns;
      const row: DiffRow = { kind, content: line.content.replace(/\r?\n$/, "") };
      if (line.oldLine !== undefined) row.oldLine = line.oldLine;
      if (line.newLine !== undefined) row.newLine = line.newLine;
      if (runs) row.runs = runs;
      return row;
    }),
  }));
}

/** Adds a later page, joining a hunk the page break split. */
export function appendHunks(shown: HunkView[], page: FileDiffPage): HunkView[] {
  const next = pageHunks(page);
  const last = shown.at(-1);
  const first = next[0];
  if (last && first && page.hunks[0]?.continuedBefore) {
    return [
      ...shown.slice(0, -1),
      { header: last.header, rows: [...last.rows, ...first.rows] },
      ...next.slice(1),
    ];
  }
  return [...shown, ...next];
}

/** Lines picked in one file, on one side, from the first tap to the last. */
export interface Selection {
  path: string;
  side: Side;
  anchor: number;
  head: number;
}

export function selectionRange(selection: Selection): { start: number; end: number } {
  return {
    start: Math.min(selection.anchor, selection.head),
    end: Math.max(selection.anchor, selection.head),
  };
}

/** How many lines a selection covers. */
export function selectionLength(selection: Selection): number {
  const { start, end } = selectionRange(selection);
  return end - start + 1;
}

/**
 * The selection after a tap on a row's number. The first tap picks a line;
 * a tap on another line on the same side in the same file extends to it;
 * a tap on the only line picked clears it. Anything else starts over.
 */
export function tapLine(current: Selection | undefined, path: string, row: DiffRow): Selection | undefined {
  if (current && current.path === path) {
    const line = lineOn(row, current.side);
    if (line !== undefined) {
      if (current.anchor === current.head && current.anchor === line) return undefined;
      return { ...current, head: line };
    }
  }
  const side = sideOf(row);
  const line = lineOn(row, side);
  return line === undefined ? undefined : { path, side, anchor: line, head: line };
}

/** Whether a row is among the selected lines. */
export function selected(selection: Selection | undefined, path: string, row: DiffRow): boolean {
  if (!selection || selection.path !== path) return false;
  const line = lineOn(row, selection.side);
  // A removed line has no new number and an added one no old one, so only
  // rows on the selection's side count.
  if (line === undefined) return false;
  const { start, end } = selectionRange(selection);
  return line >= start && line <= end;
}

/**
 * Why a selection can't take a comment, or undefined. Its lines must all be
 * among the `rows` shown, as the server checks: a range can't span the
 * unchanged lines between two hunks.
 */
export function selectionProblem(selection: Selection, rows: readonly DiffRow[]): string | undefined {
  if (selectionLength(selection) > MAX_COMMENT_LINES)
    return `A comment covers at most ${MAX_COMMENT_LINES} lines.`;
  const { start, end } = selectionRange(selection);
  const shown = new Set<number>();
  for (const row of rows) {
    const line = lineOn(row, selection.side);
    if (line !== undefined && line >= start && line <= end) shown.add(line);
  }
  return shown.size < end - start + 1 ? "Pick lines from one block of changes." : undefined;
}

/**
 * The index of the row a note on lines `start`–`end` goes under: the last
 * shown row on that side within them, or -1 when none of them is shown.
 */
export function rowFor(rows: readonly DiffRow[], side: Side, start: number, end: number): number {
  let found = -1;
  rows.forEach((row, index) => {
    const line = lineOn(row, side);
    if (line !== undefined && line >= start && line <= end) found = index;
  });
  return found;
}

/** The directory part of a path, ending in a slash, and its name. */
export function splitPath(path: string): { dir: string; name: string } {
  const at = path.lastIndexOf("/");
  return { dir: path.slice(0, at + 1), name: path.slice(at + 1) };
}

/** Whether a file's changes can be read as text. */
export function readable(file: DiffFileSummary): boolean {
  return file.contentState === "text";
}

/** A short word for a file that isn't drawn as text, or for a new or removed one. */
export function fileTag(file: DiffFileSummary): string | undefined {
  switch (file.contentState) {
    case "binary":
      return "Binary";
    case "too_large":
      return "Too large";
    case "conflict":
      return "Conflict";
    case "unsupported_kind":
      return file.reason === "symlink" ? "Symlink" : file.reason === "submodule" ? "Submodule" : "Not text";
    case "unsupported_transform":
      return "Filtered";
    case "unavailable":
      return "Unavailable";
    default:
      break;
  }
  if (file.change === "added") return "New";
  if (file.change === "deleted") return "Deleted";
  if (file.change === "mode_changed") return "Mode";
  return undefined;
}

/** Why a file's diff isn't shown, in a sentence. */
export function unreadableReason(file: DiffFileSummary): string {
  switch (file.contentState) {
    case "binary":
      return "Binary file. Changes aren’t shown.";
    case "too_large":
      return "This file is too large to show.";
    case "conflict":
      return "This file has a merge conflict.";
    case "intent_to_add":
      return "This file is staged as intent to add.";
    case "unsupported_kind":
      return "This kind of file isn’t shown.";
    case "unsupported_transform":
      return "Git filters this file, so its changes aren’t shown.";
    default:
      return "This file can’t be read right now.";
  }
}

export interface LineCounts {
  additions: number;
  deletions: number;
}

/**
 * A file's added and removed lines: the server's count when it gives one,
 * else a count of its hunks once all of them are read (`whole`).
 */
export function fileCounts(
  file: DiffFileSummary,
  hunks: readonly HunkView[],
  whole: boolean,
): LineCounts | undefined {
  if (file.additions !== undefined || file.deletions !== undefined) {
    return { additions: file.additions ?? 0, deletions: file.deletions ?? 0 };
  }
  if (!whole) return undefined;
  const counts = { additions: 0, deletions: 0 };
  for (const row of hunks.flatMap((hunk) => hunk.rows)) {
    if (row.kind === "addition") counts.additions += 1;
    if (row.kind === "deletion") counts.deletions += 1;
  }
  return counts;
}

export interface ChangeSummary {
  files: number;
  /** All files' lines, when the server counted every file. */
  counts?: LineCounts;
  /** More files exist than were read. */
  more: boolean;
}

export function summarize(files: readonly DiffFileSummary[], more: boolean): ChangeSummary {
  const counted =
    !more && files.every((file) => file.additions !== undefined || file.deletions !== undefined);
  if (!counted) return { files: files.length, more };
  const counts = { additions: 0, deletions: 0 };
  for (const file of files) {
    counts.additions += file.additions ?? 0;
    counts.deletions += file.deletions ?? 0;
  }
  return { files: files.length, counts, more };
}

export function filesLabel(summary: ChangeSummary): string {
  const count = `${summary.files}${summary.more ? "+" : ""}`;
  return summary.files === 1 && !summary.more ? "1 file" : `${count} files`;
}
