// A file in the session's folder: where the reader asked to open it, how it
// reads as lines, and the lines picked for a comment.
import { ApiError } from "../api/client";
import type { WorkspaceFileRead } from "../api/types";
import { MAX_COMMENT_LINES } from "../changes/comments";
import { codeLines, coloredLineList, type Run } from "../markdown/highlight";

/** A file to show, and the lines to bring into view and mark. */
export interface FileLocation {
  path: string;
  line?: number;
  end?: number;
}

const encodePath = (path: string) => encodeURIComponent(path).replaceAll("%2F", "/");

/** The session page's fragment for a file: #file=apps/web/watch.ts&line=88&end=89. */
export function fileHash(location: FileLocation): string {
  let hash = `#file=${encodePath(location.path)}`;
  if (location.line) {
    hash += `&line=${location.line}`;
    if (location.end && location.end > location.line) hash += `&end=${location.end}`;
  }
  return hash;
}

const positive = (value: string | null): number | undefined => {
  if (!value || !/^\d{1,9}$/.test(value)) return undefined;
  const number = Number(value);
  return number > 0 ? number : undefined;
};

/** The file a fragment names, or undefined when it names none. */
export function parseFileHash(hash: string): FileLocation | undefined {
  if (!hash.startsWith("#file=")) return undefined;
  const params = new URLSearchParams(hash.slice(1));
  const path = params.get("file") ?? "";
  if (!validPath(path)) return undefined;
  const line = positive(params.get("line"));
  const end = line ? positive(params.get("end")) : undefined;
  return {
    path,
    ...(line ? { line } : {}),
    ...(line && end && end > line ? { end } : {}),
  };
}

/** Whether a path names something inside the session's folder, as the server takes it. */
export function validPath(path: string): boolean {
  if (!path || path.startsWith("/") || path.endsWith("/") || path.includes("\\") || path.includes("\0"))
    return false;
  return path.split("/").every((part) => part !== "" && part !== "." && part !== "..");
}

/**
 * A tool's path as a path in the session's folder, or undefined when it's
 * elsewhere. Tools take paths relative to the folder or absolute; `~/`
 * means the server's home.
 */
export function folderPath(path: string, cwd: string, home?: string): string | undefined {
  let value = path.trim();
  if (value === "~" || value.startsWith("~/")) {
    if (!home) return undefined;
    value = home.replace(/\/$/, "") + value.slice(1);
  }
  if (value.startsWith("/")) {
    const root = cwd.replace(/\/+$/, "");
    if (!root || !value.startsWith(`${root}/`)) return undefined;
    value = value.slice(root.length + 1);
  }
  const parts: string[] = [];
  for (const part of value.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      if (parts.length === 0) return undefined;
      parts.pop();
    } else parts.push(part);
  }
  const relative = parts.join("/");
  return validPath(relative) ? relative : undefined;
}

/** Whether a file reads as Markdown, which opens as a preview. */
export function isMarkdown(path: string): boolean {
  return /\.(md|markdown|mdx)$/i.test(path);
}

/** One line of a file, numbered from 1, in its colors when the server gave them. */
export interface FileLine {
  number: number;
  text: string;
  runs?: Run[];
}

export function fileLines(read: Pick<WorkspaceFileRead, "content" | "highlight">): FileLine[] {
  const texts = codeLines(read.content).map((line) => line.replace(/\r$/, ""));
  const colors = read.highlight ? coloredLineList(texts, read.highlight) : undefined;
  return texts.map((text, index) => {
    const runs = colors?.[index];
    return runs ? { number: index + 1, text, runs } : { number: index + 1, text };
  });
}

/** What a cut-off read says at its end, or undefined when the whole file came. */
export function truncationNote(
  read: Pick<WorkspaceFileRead, "truncated" | "truncationReason" | "returnedLines">,
): string | undefined {
  if (!read.truncated) return undefined;
  if (read.truncationReason === "line_limit")
    return `Showing the first ${read.returnedLines.toLocaleString("en-US")} lines. The rest of the file is too long to show.`;
  return "Showing the start of the file. The rest is too large to show.";
}

/** Why a file can't be shown, in a sentence, or undefined for an unexpected failure. */
export function fileProblem(error: unknown): string | undefined {
  if (!(error instanceof ApiError)) return undefined;
  switch (error.code) {
    case "not_found":
      return "This file isn’t in the session’s folder.";
    case "binary_file":
      return "This file isn’t text, so it can’t be shown.";
    case "not_file":
    case "not_directory":
      return "This isn’t a file.";
    case "outside_workspace":
    case "symlink_traversal":
    case "invalid_path":
      return "This file is outside the session’s folder.";
    case "permission_denied":
      return "Kit isn’t allowed to read this file.";
    case "limit_exceeded":
      return "This file is too large to show.";
    case "stale_workspace":
      return "The session moved to another folder.";
    case "capacity_exceeded":
    case "rate_limited":
      return "The server is busy. Try again in a moment.";
    default:
      return undefined;
  }
}

/** Lines picked in a file, from the first tap to the last. */
export interface LinePick {
  anchor: number;
  head: number;
}

export function pickRange(pick: LinePick): { start: number; end: number } {
  return { start: Math.min(pick.anchor, pick.head), end: Math.max(pick.anchor, pick.head) };
}

/**
 * The pick after a tap on a line number: the first tap picks a line, a tap
 * on another extends to it, and a tap on the only line picked clears it.
 */
export function tapFileLine(current: LinePick | undefined, line: number): LinePick | undefined {
  if (!current) return { anchor: line, head: line };
  if (current.anchor === current.head && current.anchor === line) return undefined;
  return { ...current, head: line };
}

export function picked(pick: LinePick | undefined, line: number): boolean {
  if (!pick) return false;
  const { start, end } = pickRange(pick);
  return line >= start && line <= end;
}

/** Why picked lines can't take a comment, or undefined. */
export function pickProblem(pick: LinePick, lineCount: number): string | undefined {
  const { start, end } = pickRange(pick);
  if (end - start + 1 > MAX_COMMENT_LINES) return `A comment covers at most ${MAX_COMMENT_LINES} lines.`;
  if (start < 1 || end > lineCount) return "Pick lines that are shown.";
  return undefined;
}
