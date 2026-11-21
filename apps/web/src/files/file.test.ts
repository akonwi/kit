import { describe, expect, test } from "bun:test";
import { ApiError } from "../api/client";
import {
  blockLines,
  blockOf,
  fileHash,
  fileLines,
  fileProblem,
  folderPath,
  isMarkdown,
  parseFileHash,
  pickProblem,
  tapFileLine,
} from "./file";

describe("file fragments", () => {
  test("name a file and the lines to show, keeping slashes readable", () => {
    expect(fileHash({ path: "apps/web/src/watch.ts" })).toBe("#file=apps/web/src/watch.ts");
    expect(fileHash({ path: "docs/a b&c.md", line: 88, end: 89 })).toBe(
      "#file=docs/a%20b%26c.md&line=88&end=89",
    );
    expect(fileHash({ path: "x.ts", line: 4, end: 4 })).toBe("#file=x.ts&line=4");
  });

  test("read back what they name", () => {
    expect(parseFileHash("#file=apps/web/src/watch.ts")).toEqual({ path: "apps/web/src/watch.ts" });
    expect(parseFileHash("#file=docs/a%20b%26c.md&line=88&end=89")).toEqual({
      path: "docs/a b&c.md",
      line: 88,
      end: 89,
    });
    expect(parseFileHash("#file=x.ts&line=0")).toEqual({ path: "x.ts" });
    expect(parseFileHash("#file=x.ts&end=9")).toEqual({ path: "x.ts" });
  });

  test("name nothing outside the session's folder", () => {
    expect(parseFileHash("#changes")).toBeUndefined();
    expect(parseFileHash("#file=")).toBeUndefined();
    expect(parseFileHash("#file=/etc/passwd")).toBeUndefined();
    expect(parseFileHash("#file=../secret")).toBeUndefined();
    expect(parseFileHash("#file=a/./b")).toBeUndefined();
  });
});

describe("folderPath", () => {
  const cwd = "/Users/me/kit";
  test("takes paths relative to the folder, or absolute inside it", () => {
    expect(folderPath("apps/web/watch.ts", cwd)).toBe("apps/web/watch.ts");
    expect(folderPath("./apps//web/../web/watch.ts", cwd)).toBe("apps/web/watch.ts");
    expect(folderPath("/Users/me/kit/go.mod", cwd)).toBe("go.mod");
    expect(folderPath("~/kit/go.mod", cwd, "/Users/me")).toBe("go.mod");
  });

  test("refuses paths elsewhere", () => {
    expect(folderPath("/Users/me/kitten/go.mod", cwd)).toBeUndefined();
    expect(folderPath("/etc/hosts", cwd)).toBeUndefined();
    expect(folderPath("../other/file", cwd)).toBeUndefined();
    expect(folderPath("~/kit/go.mod", cwd)).toBeUndefined();
    expect(folderPath(".", cwd)).toBeUndefined();
  });
});

test("Markdown files open as a preview", () => {
  expect(isMarkdown("docs/pairing.md")).toBe(true);
  expect(isMarkdown("README.MARKDOWN")).toBe(true);
  expect(isMarkdown("src/main.ts")).toBe(false);
});

describe("fileLines", () => {
  test("splits at line ends and drops the carriage return before one", () => {
    expect(fileLines("a\r\nb\n")).toEqual(["a", "b"]);
    expect(fileLines("a\n\nb")).toEqual(["a", "", "b"]);
    expect(fileLines("")).toEqual([]);
  });
});

test("blocks hold 128 lines, the last what's left", () => {
  expect(blockOf(1)).toBe(0);
  expect(blockOf(128)).toBe(0);
  expect(blockOf(129)).toBe(1);
  expect(blockLines(0, 3)).toEqual([1, 2, 3]);
  expect(blockLines(1, 130)).toEqual([129, 130]);
  expect(blockLines(2, 130)).toEqual([]);
});

test("failures read as sentences", () => {
  expect(fileProblem(new ApiError(404, "not_found", "missing"))).toBe(
    "This file isn’t in the session’s folder.",
  );
  expect(fileProblem(new ApiError(404, "http_404", "The server answered 404."))).toBe(
    "This server’s Kit needs an update to open files.",
  );
  expect(fileProblem(new Error("boom"))).toBeUndefined();
});

describe("picking lines", () => {
  test("a tap picks, a second extends, and a tap on the only line clears", () => {
    const first = tapFileLine(undefined, 88);
    expect(first).toEqual({ anchor: 88, head: 88 });
    expect(tapFileLine(first, 91)).toEqual({ anchor: 88, head: 91 });
    expect(tapFileLine(first, 88)).toBeUndefined();
    expect(tapFileLine({ anchor: 88, head: 91 }, 85)).toEqual({ anchor: 88, head: 85 });
  });

  test("a comment covers shown lines, at most 200", () => {
    expect(pickProblem({ anchor: 1, head: 200 }, 300)).toBeUndefined();
    expect(pickProblem({ anchor: 1, head: 201 }, 300)).toBe("A comment covers at most 200 lines.");
    expect(pickProblem({ anchor: 10, head: 12 }, 11)).toBe("Pick lines that are shown.");
  });
});
