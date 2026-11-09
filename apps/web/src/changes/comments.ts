// Comments on changed lines: the session's annotations (ADR 0022) that go
// with the next message, as the composer and the Changes screen show them.
import type {
  Annotation,
  AnnotationAnchor,
  AnnotationSummary,
  DiffAnchor,
  SubmittedAnnotation,
} from "../api/types";

/** The most comments one message carries, as the server allows. */
export const MAX_COMMENTS = 64;
/** The most lines one comment covers. */
export const MAX_COMMENT_LINES = 200;
/** The longest comment, in UTF-8 bytes. */
export const MAX_COMMENT_BYTES = 16 * 1024;

export interface Comment {
  id: number;
  anchor: AnnotationAnchor;
  /** The diff lines it's on; a comment on a file outside a diff has none. */
  diff?: DiffAnchor;
  /** Whether its diff compares commits; one on uncommitted changes doesn't. */
  committed: boolean;
  path: string;
  startLine: number;
  endLine: number;
  body: string;
  /** Whether `body` is the whole comment; a snapshot carries only its start. */
  complete: boolean;
  /** The source changed under it, so it can't be sent as it is. */
  stale: boolean;
  staleReason?: string;
}

function place(anchor: AnnotationAnchor): Pick<Comment, "diff" | "path" | "startLine" | "endLine"> {
  const diff = anchor.workingTreeDiff;
  if (diff) return { diff, path: diff.path, startLine: diff.startLine, endLine: diff.endLine };
  const file = anchor.workspaceFile;
  return { path: file?.path ?? "", startLine: file?.startLine ?? 0, endLine: file?.endLine ?? 0 };
}

export function fromSummary(summary: AnnotationSummary): Comment {
  return {
    id: summary.id,
    anchor: summary.anchor,
    ...place(summary.anchor),
    committed: Boolean(summary.diffTarget),
    body: summary.bodyPreview,
    complete: false,
    stale: summary.stale === true,
    staleReason: summary.staleReason,
  };
}

/** A whole comment; `stale` is the server's check, when it made one. */
export function fromAnnotation(annotation: Annotation, stale = annotation.stale === true): Comment {
  return {
    id: annotation.id,
    anchor: annotation.anchor,
    ...place(annotation.anchor),
    committed: Boolean(annotation.diffTarget),
    body: annotation.body,
    complete: true,
    stale,
    staleReason: stale ? annotation.staleReason : undefined,
  };
}

export function lineRange(start: number, end: number): string {
  return start === end ? `${start}` : `${start}–${end}`;
}

export function baseName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

/** `watch.ts:89–91`, as a chip or a sent card names a comment. */
export function commentLabel(comment: { path: string; startLine: number; endLine: number }): string {
  return `${baseName(comment.path)}:${lineRange(comment.startLine, comment.endLine)}`;
}

/** Lines 89–91, or Line 89. */
export function linesLabel(start: number, end: number): string {
  return start === end ? `Line ${start}` : `Lines ${start}–${end}`;
}

/**
 * Why a comment can't be sent, and where it was: "File changed · was line
 * 89". Uncommitted changes are compared as a whole, so a change to any file
 * moves every comment on them.
 */
export function staleLabel(comment: Comment): string {
  const reason =
    comment.staleReason === "workspace_changed"
      ? "Folder changed"
      : comment.staleReason === "target_changed"
        ? comment.committed
          ? "Commits changed"
          : "Files changed"
        : comment.staleReason === "resource_unavailable"
          ? "File unavailable"
          : "File changed";
  const lines = comment.startLine === comment.endLine ? "line" : "lines";
  return `${reason} · was ${lines} ${lineRange(comment.startLine, comment.endLine)}`;
}

/** Whether a comment is on uncommitted changes, which the server holds only briefly. */
export function onWorkingTree(comment: Comment): boolean {
  return Boolean(comment.diff) && !comment.committed;
}

/** What the composer says while comments are stale, or undefined. */
export function staleNotice(comments: readonly Comment[]): string | undefined {
  const count = comments.filter((comment) => comment.stale).length;
  if (count === 0) return undefined;
  return count === 1
    ? "A file changed under one comment. Select a new range or delete it before sending."
    : `Files changed under ${count} comments. Select new ranges or delete them before sending.`;
}

/** Why the comments can't go with a message, or undefined when they can. */
export function commentsBlocked(comments: readonly Comment[]): string | undefined {
  if (comments.some((comment) => comment.stale)) return staleNotice(comments);
  if (comments.length > MAX_COMMENTS) return `A message can carry up to ${MAX_COMMENTS} comments.`;
  return undefined;
}

/** A sent comment, as a card in the message names it. */
export function sentLabel(annotation: SubmittedAnnotation): string {
  return commentLabel(place(annotation.anchor));
}

/**
 * The comments after a fresh list. A list read while events changed them
 * can't say which are gone, so it only refreshes the ones it has, and not
 * those an event changed after it was asked for (`changed`).
 */
export function mergeComments(
  current: readonly Comment[],
  listed: readonly Comment[],
  whole: boolean,
  changed: (id: number) => boolean = () => false,
): Comment[] {
  if (whole) return [...listed];
  const fresh = new Map(listed.map((comment) => [comment.id, comment]));
  return current.map((comment) => (changed(comment.id) ? comment : (fresh.get(comment.id) ?? comment)));
}
