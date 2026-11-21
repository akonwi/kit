import { describe, expect, test } from "bun:test";
import { colorFile, fileLanguage, MAX_COLORED_LINE, recordRuns, toneOf } from "./colors";

describe("fileLanguage", () => {
  const known = new Set(["ts", "go", "makefile", "dockerfile", "python", "cmake", "xml", "txt"]);
  const supported = (name: string) => known.has(name);

  test("comes from the extension, then the whole name", () => {
    expect(fileLanguage("apps/web/src/main.ts", supported)).toBe("ts");
    expect(fileLanguage("Dockerfile", supported)).toBe("dockerfile");
    expect(fileLanguage("build/Makefile", supported)).toBe("makefile");
  });

  test("knows some names the highlighter doesn't", () => {
    expect(fileLanguage("GNUmakefile", supported)).toBe("makefile");
    expect(fileLanguage("CMakeLists.txt", supported)).toBe("cmake");
    expect(fileLanguage("stubs/os.pyi", supported)).toBe("python");
  });

  test("is undefined for plain text and unknown files", () => {
    expect(fileLanguage("notes.txt", supported)).toBeUndefined();
    expect(fileLanguage("main.ard", supported)).toBeUndefined();
    expect(fileLanguage(".gitignore", supported)).toBeUndefined();
  });
});

test("token kinds draw in the Mac's tones", () => {
  expect(toneOf("comment.doc")).toBe("comment");
  expect(toneOf("string.escape")).toBe("string");
  expect(toneOf("keyword.control")).toBe("keyword");
  expect(toneOf("type.builtin")).toBe("keyword");
  expect(toneOf("function.method")).toBe("keyword");
  expect(toneOf("constant.builtin")).toBe("keyword");
  expect(toneOf("number")).toBeUndefined();
  expect(toneOf("variable")).toBeUndefined();
});

test("records become runs, joined where tones meet and plain past the last", () => {
  const tones = [undefined, "keyword", "string", "keyword"] as const;
  const records = [
    { end: 5, token: 1 },
    { end: 6, token: 0 },
    { end: 9, token: 2 },
    { end: 10, token: 3 },
    { end: 11, token: 1 },
  ];
  expect(recordRuns('const "x" if = 1', records, tones)).toEqual([
    { text: "const", tone: "keyword" },
    { text: " " },
    { text: '"x"', tone: "string" },
    { text: " i", tone: "keyword" },
    { text: "f = 1" },
  ]);
});

describe("colorFile", () => {
  test("colors a file's lines in the browser's highlighter", async () => {
    const text = 'const a = "x"; // note\r\n\nreturn\n';
    const colors = await colorFile("src/a.ts", text, 3);
    expect(colors).toBeDefined();
    expect(colors?.runs(0, 'const a = "x"; // note')).toEqual([
      { text: "const ", tone: "keyword" },
      { text: "a = " },
      { text: '"x"', tone: "string" },
      { text: "; " },
      { text: "// note", tone: "comment" },
    ]);
    expect(colors?.runs(1, "")).toEqual([]);
    colors?.dispose();
    expect(colors?.runs(0, 'const a = "x"; // note')).toBeUndefined();
  });

  test("leaves a line plain whose text isn't the highlighter's", async () => {
    const colors = await colorFile("a.ts", "const a = 1\n", 1);
    expect(colors?.runs(0, "something else")).toBeUndefined();
    colors?.dispose();
  });

  test("leaves very long lines plain", async () => {
    const line = `const a = "${"x".repeat(MAX_COLORED_LINE)}";`;
    const colors = await colorFile("a.ts", line, 1);
    expect(colors?.runs(0, line)).toBeUndefined();
    colors?.dispose();
  });

  test("has no colors for a file whose lines it counts differently", async () => {
    expect(await colorFile("a.ts", "a\rb\n", 1)).toBeUndefined();
  });

  test("has no colors for a language it doesn't know", async () => {
    expect(await colorFile("main.ard", "fn main() {}\n", 1)).toBeUndefined();
  });
});
