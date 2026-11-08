import { describe, expect, test } from "bun:test";
import type { HighlightCodeBlock, HighlightCodeResult } from "../api/types";
import { askable, BOUNDS, batches, codeLines, coloredLines, createHighlighter } from "./highlight";

describe("codeLines", () => {
  test("counts lines as the server does: a final newline ends the last line", () => {
    expect(codeLines("")).toEqual([]);
    expect(codeLines("a\n")).toEqual(["a"]);
    expect(codeLines("a\nb")).toEqual(["a", "b"]);
    expect(codeLines("a\n\n")).toEqual(["a", ""]);
  });
});

describe("coloredLines", () => {
  const source = "// Reload once\nasync function reload(id: string) {\n";
  const highlight = {
    language: "typescript",
    roles: ["comment", "keyword", "function", "variable", "keywordType", "punctuation"],
    lines: [
      [0, 14, 0],
      [0, 5, 1, 6, 8, 1, 15, 6, 2, 22, 2, 3, 26, 6, 4, 33, 1, 5],
    ],
  };

  test("draws each role in the Mac's tone, and the rest as text", () => {
    expect(coloredLines(source, highlight)).toEqual([
      [{ text: "// Reload once", tone: "comment" }],
      [
        { text: "async", tone: "keyword" },
        { text: " " },
        { text: "function", tone: "keyword" },
        { text: " " },
        { text: "reload", tone: "keyword" },
        { text: "(id: " },
        { text: "string", tone: "keyword" },
        { text: ") {" },
      ],
    ]);
  });

  test("draws a role it doesn't know as text", () => {
    expect(coloredLines("x", { language: "go", roles: ["sparkle"], lines: [[0, 1, 0]] })).toEqual([
      [{ text: "x" }],
    ]);
  });

  test("counts offsets in UTF-16, as the server does, and leaves a CR unstyled", () => {
    expect(coloredLines('s := "😀"\r\n', { language: "go", roles: ["string"], lines: [[5, 4, 0]] })).toEqual([
      [{ text: "s := " }, { text: '"😀"', tone: "string" }, { text: "\r" }],
    ]);
  });

  test("refuses colors that don't describe the text", () => {
    const one = (lines: number[][], roles = ["keyword"]) =>
      coloredLines("func main", { language: "go", roles, lines });
    expect(one([])).toBeUndefined();
    expect(one([[0, 4]])).toBeUndefined();
    expect(one([[0, 4, 1]])).toBeUndefined();
    expect(one([[5, 4, 0, 0, 4, 0]])).toBeUndefined();
    expect(one([[6, 4, 0]])).toBeUndefined();
    expect(one([[0, 4.5, 0]])).toBeUndefined();
  });
});

describe("askable", () => {
  test("asks only for labelled blocks within the server's bounds", () => {
    expect(askable("ts", "let a = 1")).toBe(true);
    expect(askable(" ", "let a = 1")).toBe(false);
    expect(askable("ts", "")).toBe(false);
    expect(askable("x".repeat(65), "a")).toBe(false);
    expect(askable("ts", "é".repeat(BOUNDS.blockBytes / 2 + 1))).toBe(false);
  });
});

describe("batches", () => {
  test("splits at the server's block count and total size", () => {
    const small = Array.from({ length: 130 }, (_, i) => ({ language: "go", source: `${i}` }));
    expect(batches(small).map((batch) => batch.length)).toEqual([128, 2]);
    const big = (n: number) => ({ language: "go", source: "x".repeat(n) });
    expect(batches([big(400_000), big(400_000), big(10)]).map((batch) => batch.length)).toEqual([1, 2]);
  });
});

describe("createHighlighter", () => {
  const colors = { language: "go", roles: ["keyword"], lines: [[0, 4, 0]] };
  const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

  test("asks for the blocks shown together in one request, and once", async () => {
    const sent: HighlightCodeBlock[][] = [];
    const highlighter = createHighlighter(async (blocks) => {
      sent.push(blocks);
      return { blocks: blocks.map((block) => (block.language === "go" ? { highlight: colors } : {})) };
    });
    const go = highlighter.colors("go", "func main");
    const text = highlighter.colors("text", "plain words");
    highlighter.colors("", "unlabelled");
    expect(go()).toBeUndefined();
    await settle();
    expect(sent).toEqual([
      [
        { language: "go", source: "func main" },
        { language: "text", source: "plain words" },
      ],
    ]);
    expect(go()).toEqual(colors);
    expect(text()).toBeUndefined();
    expect(highlighter.colors("go", "func main")()).toEqual(colors);
    await settle();
    expect(sent.length).toBe(1);
  });

  test("leaves a failed batch plain and asks again next time", async () => {
    let calls = 0;
    const highlighter = createHighlighter(async (blocks): Promise<HighlightCodeResult> => {
      calls++;
      if (calls === 1) throw new Error("offline");
      if (calls === 2) return { blocks: [] };
      return { blocks: blocks.map(() => ({ highlight: colors })) };
    });
    const first = highlighter.colors("go", "func main");
    await settle();
    expect(first()).toBeUndefined();
    const second = highlighter.colors("go", "func main");
    await settle();
    expect(second()).toBeUndefined();
    const third = highlighter.colors("go", "func main");
    await settle();
    expect([calls, third()]).toEqual([3, colors]);
  });

  test("keeps refused blocks plain, and stops asking a server without the operation", async () => {
    let calls = 0;
    const refuse = (status: number) => Object.assign(new Error("refused"), { status });
    const highlighter = createHighlighter(async () => {
      calls++;
      throw refuse(calls === 1 ? 413 : 404);
    });
    highlighter.colors("go", "func main");
    await settle();
    highlighter.colors("go", "func main");
    await settle();
    expect(calls).toBe(1);
    highlighter.colors("go", "func other");
    await settle();
    expect(highlighter.colors("go", "func third")()).toBeUndefined();
    await settle();
    expect(calls).toBe(2);
  });
});
