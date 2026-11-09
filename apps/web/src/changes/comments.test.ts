import { describe, expect, test } from "bun:test";
import type { Annotation } from "../api/types";
import {
  type Comment,
  commentLabel,
  commentsBlocked,
  fromAnnotation,
  mergeComments,
  staleLabel,
  staleNotice,
} from "./comments";

function annotation(id: number, value: Partial<Annotation> = {}): Annotation {
  return {
    id,
    sessionId: "s1",
    body: `Comment ${id}`,
    anchor: {
      kind: "working_tree_diff",
      workingTreeDiff: {
        targetId: "difftarget_A",
        targetRevision: "diffrev_A",
        path: "apps/web/src/session/watch.ts",
        fileRevision: "diff_file_A",
        side: "new",
        startLine: 89,
        endLine: 91,
      },
    },
    preview: { startLine: 89, endLine: 91, text: "a\nb\nc\n" },
    ...value,
  };
}

const comment = (id: number, value: Partial<Comment> = {}): Comment => ({
  ...fromAnnotation(annotation(id)),
  ...value,
});

describe("comments", () => {
  test("names a comment by its file and lines", () => {
    expect(commentLabel(comment(1))).toBe("watch.ts:89–91");
    expect(commentLabel(comment(1, { startLine: 89, endLine: 89 }))).toBe("watch.ts:89");
  });

  test("says why a stale comment can't go, and where it was", () => {
    expect(
      staleLabel(comment(1, { stale: true, staleReason: "file_changed", startLine: 89, endLine: 89 })),
    ).toBe("File changed · was line 89");
    expect(staleLabel(comment(1, { stale: true, staleReason: "target_changed" }))).toBe(
      "Files changed · was lines 89–91",
    );
    expect(staleLabel(comment(1, { stale: true, staleReason: "target_changed", committed: true }))).toBe(
      "Commits changed · was lines 89–91",
    );
  });

  test("blocks sending while a comment is stale or there are too many", () => {
    expect(commentsBlocked([comment(1)])).toBeUndefined();
    expect(staleNotice([comment(1, { stale: true }), comment(2)])).toBe(
      "A file changed under one comment. Select a new range or delete it before sending.",
    );
    expect(commentsBlocked([comment(1, { stale: true }), comment(2, { stale: true })])).toBe(
      "Files changed under 2 comments. Select new ranges or delete them before sending.",
    );
    expect(commentsBlocked(Array.from({ length: 65 }, (_, index) => comment(index + 1)))).toBe(
      "A message can carry up to 64 comments.",
    );
  });

  test("a list read across a comment event only refreshes the comments it has", () => {
    const current = [comment(1), comment(2)];
    const listed = [comment(1, { stale: true })];
    expect(mergeComments(current, listed, true)).toEqual(listed);
    expect(mergeComments(current, listed, false)).toEqual([comment(1, { stale: true }), comment(2)]);
    // An edit made after the list was asked for wins over the list's older copy.
    expect(mergeComments(current, listed, false, (id) => id === 1)).toEqual(current);
  });
});
