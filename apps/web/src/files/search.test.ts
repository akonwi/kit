import { describe, expect, test } from "bun:test";
import { markedParts, searchPaths } from "./search";

const paths = (query: string, list: string[]) => searchPaths(query, list).map((match) => match.path);

describe("searchPaths", () => {
  test("ranks an exact match, then a prefix, then a substring, then the letters in order", () => {
    expect(paths("src", ["other/src.go", "src/main.go", "src/", "src", "scratch"])).toEqual([
      "src",
      "src/main.go",
      "src/",
      "other/src.go",
      "scratch",
    ]);
    expect(paths("tsh", ["internal/tui/app.go", "internal/tui/shell.go"])).toEqual(["internal/tui/shell.go"]);
    expect(paths("msw", ["README.md", "main.swift"])).toEqual(["main.swift"]);
  });

  test("ignores case and the space around a query, and keeps every path for no query", () => {
    expect(paths("  ReadMe ", ["docs/readme.md", "README.md"])).toEqual(["README.md", "docs/readme.md"]);
    expect(paths("", ["b", "a"])).toEqual(["b", "a"]);
  });

  test("says where each path matched", () => {
    expect(searchPaths("watch", ["apps/web/src/session/watch.ts"])).toEqual([
      { path: "apps/web/src/session/watch.ts", matches: [21, 22, 23, 24, 25] },
    ]);
    expect(searchPaths("wtac", ["docs/worktree-attach.md"])).toEqual([
      { path: "docs/worktree-attach.md", matches: [5, 9, 14, 18] },
    ]);
  });

  test("returns at most the limit", () => {
    expect(searchPaths("a", ["a1", "a2", "a3"], 2).map((match) => match.path)).toEqual(["a1", "a2"]);
    expect(searchPaths("", ["a1", "a2", "a3"], 2).map((match) => match.path)).toEqual(["a1", "a2"]);
  });
});

describe("markedParts", () => {
  test("splits a name into matched and unmatched runs, offset into its path", () => {
    expect(markedParts("watch.ts", [21, 22, 23, 24, 25], 21)).toEqual([
      { text: "watch", hit: true },
      { text: ".ts", hit: false },
    ]);
    expect(markedParts("src/", [], 0)).toEqual([{ text: "src/", hit: false }]);
  });
});
