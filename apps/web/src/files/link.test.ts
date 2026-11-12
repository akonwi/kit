import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { parseMarkdown } from "../markdown/parse";
import { folderPath } from "./file";
import { linkedLocation, linkTarget, withLines } from "./link";

// The cases every client's parser reads.
const fixture = join(import.meta.dir, "../../../../testdata/file-links/destinations.tsv");
const cases = readFileSync(fixture, "utf8")
  .split("\n")
  .filter((line) => line && !line.startsWith("# "))
  .map((line) => line.split("\t") as [string, string, string]);

describe("linkedLocation", () => {
  test("the fixture is present", () => {
    expect(cases.length).toBeGreaterThanOrEqual(25);
  });

  test.each(cases)("%s names %s at %s", (destination, path, range) => {
    const [start, end] = range === "-" ? [] : range.split("-").map(Number);
    const expected =
      path === "-"
        ? undefined
        : { path, ...(start ? { line: start } : {}), ...(end && start && end > start ? { end } : {}) };
    expect(linkedLocation(destination)).toEqual(expected);
  });

  test("reads a destination as the Markdown parser gives it", () => {
    const tree = parseMarkdown("[a](src/a%20b.go#L3-L5) [b](docs/ü.md#L7)");
    const urls = JSON.stringify(tree).match(/"url":"[^"]+"/g);
    expect(urls).toEqual(['"url":"src/a%20b.go#L3-L5"', '"url":"docs/ü.md#L7"']);
    expect(linkedLocation("src/a%20b.go#L3-L5")).toEqual({ path: "src/a b.go", line: 3, end: 5 });
    expect(linkedLocation("docs/ü.md#L7")).toEqual({ path: "docs/ü.md", line: 7 });
  });
});

test("withLines writes a path with its lines as a link names them", () => {
  expect(withLines({ path: "src/a.go", line: 42, end: 50 })).toBe("src/a.go#L42-L50");
  expect(withLines({ path: "src/a.go", line: 42 })).toBe("src/a.go#L42");
  expect(withLines({ path: "src/a.go" })).toBe("src/a.go");
});

describe("linkTarget", () => {
  const resolve = (path: string) => folderPath(path, "/repo", "/home/me");

  test("places a link in the session's folder, keeping its lines", () => {
    expect(linkTarget({ path: "src/a.go", line: 3, end: 5 }, undefined, resolve)).toEqual({
      path: "src/a.go",
      line: 3,
      end: 5,
    });
    expect(linkTarget({ path: "/repo/src/a.go", line: 3 }, undefined, resolve)).toEqual({
      path: "src/a.go",
      line: 3,
    });
    expect(linkTarget({ path: "/etc/hosts", line: 3 }, undefined, resolve)).toBeUndefined();
  });

  test("starts a Markdown file's relative links from its folder", () => {
    expect(linkTarget({ path: "../README.md", line: 2 }, "docs/guide", resolve)).toEqual({
      path: "docs/README.md",
      line: 2,
    });
    expect(linkTarget({ path: "a.md" }, "", resolve)).toEqual({ path: "a.md" });
    expect(linkTarget({ path: "/repo/b.md" }, "docs", resolve)).toEqual({ path: "b.md" });
    expect(linkTarget({ path: "../../x.md" }, "docs", resolve)).toBeUndefined();
  });
});
