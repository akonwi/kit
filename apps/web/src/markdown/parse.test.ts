import { describe, expect, test } from "bun:test";
import type { Blockquote, Code } from "mdast";
import { calloutMarker, calloutOf, fenceClosed, infoString, parseMarkdown, safeHref } from "./parse";

describe("parseMarkdown", () => {
  test("keeps raw HTML as an html node for the renderer to show as text", () => {
    const tree = parseMarkdown('<img src=x onerror="alert(1)">');
    expect(tree.children[0]?.type).toBe("html");
  });

  test("parses GitHub tables and task lists", () => {
    const tree = parseMarkdown("| a | b |\n| - | - |\n| 1 | 2 |\n\n- [x] done");
    expect(tree.children.map((node) => node.type)).toEqual(["table", "list"]);
  });
});

describe("safeHref", () => {
  test("opens only web and mail addresses", () => {
    expect(safeHref("https://example.com/a")).toBe("https://example.com/a");
    expect(safeHref("mailto:a@example.com")).toBe("mailto:a@example.com");
    expect(safeHref("javascript:alert(1)")).toBeUndefined();
    expect(safeHref("data:text/html,hi")).toBeUndefined();
    expect(safeHref("/relative")).toBeUndefined();
  });
});

function quote(text: string): Blockquote {
  const node = parseMarkdown(text).children[0];
  if (node?.type !== "blockquote") throw new Error("not a quote");
  return node;
}

function code(text: string): Code {
  const node = parseMarkdown(text).children[0];
  if (node?.type !== "code") throw new Error("not code");
  return node;
}

describe("calloutOf", () => {
  test("reads the kind and title, and keeps the rest as the body", () => {
    const text = "> [!WARNING] Two tests are flaky\n> Rerun them first.\n>\n> - one\n> - two";
    const callout = calloutOf(quote(text), text);
    expect(callout?.kind).toBe("warning");
    expect(callout?.title).toBe("Two tests are flaky");
    expect(callout?.blocks.map((block) => block.type)).toEqual(["paragraph", "list"]);
    const first = callout?.blocks[0];
    expect(first?.type === "paragraph" && first.children).toEqual([
      { type: "text", value: "Rerun them first." },
    ]);
  });

  test("reads a marker in any case, with only a title", () => {
    const text = "> [!tip] It's safe to rerun";
    expect(calloutOf(quote(text), text)).toEqual({ kind: "tip", title: "It's safe to rerun", blocks: [] });
  });

  test("leaves an unknown marker and an ordinary quote as quotes", () => {
    for (const text of ["> [!DANGER]\n> Not a kind.", "> Just a quote.", "> [!NOTE]x"]) {
      expect(calloutOf(quote(text), text)).toBeUndefined();
    }
  });
});

describe("calloutMarker", () => {
  test("needs a space or the end after the marker", () => {
    expect(calloutMarker("[!NOTE]")).toEqual({ kind: "note", title: "" });
    expect(calloutMarker("  [!CAUTION]   Mind the gap ")).toEqual({ kind: "caution", title: "Mind the gap" });
    expect(calloutMarker("[!NOTE]:")).toBeUndefined();
  });
});

describe("fenceClosed", () => {
  test("tells a streaming fence from a closed one", () => {
    const closed = "```bars\n- a: 1\n```";
    const open = "```bars\n- a: 1\n- b: 2";
    const longer = "````ts\nlet a = 1\n```\n````";
    expect(fenceClosed(code(closed), closed)).toBe(true);
    expect(fenceClosed(code(open), open)).toBe(false);
    expect(fenceClosed(code(longer), longer)).toBe(true);
    expect(fenceClosed(code("```ts"), "```ts")).toBe(false);
  });
});

describe("infoString", () => {
  test("keeps the attributes after the language", () => {
    const text = '```bars title="Routes"\n- a: 1\n```';
    expect(infoString(code(text))).toBe('bars title="Routes"');
  });
});
