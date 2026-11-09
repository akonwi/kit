import { describe, expect, test } from "bun:test";
import type { ToolCall } from "./projection";
import { formatDuration, humanize, presentTool } from "./tools";

function call(value: Partial<ToolCall> & Pick<ToolCall, "name">): ToolCall {
  return { id: "c1", output: "", attachments: [], failed: false, status: "done", ...value };
}

describe("presentTool", () => {
  test("reads a file with the lines it returned", () => {
    expect(
      presentTool(
        call({ name: "read", arguments: '{"path":"src/stream.ts","offset":210}', output: "a\nb\nc\n" }),
      ),
    ).toEqual({
      title: "Read file",
      chip: "src/stream.ts:210–212",
      icon: "file",
      facts: [],
      path: true,
      timed: false,
      file: { path: "src/stream.ts", line: 210 },
    });
  });

  test("names the file an edit or write worked on, which opens at the top", () => {
    expect(
      presentTool(call({ name: "edit", arguments: '{"path":"a.ts","oldText":"a","newText":"b"}' })).file,
    ).toEqual({
      path: "a.ts",
    });
    expect(presentTool(call({ name: "write", arguments: '{"path":"b.ts","content":"x"}' })).file).toEqual({
      path: "b.ts",
    });
    expect(presentTool(call({ name: "read", arguments: '{"path":"c.ts"}' })).file).toEqual({ path: "c.ts" });
  });

  test("counts an edit's changed lines", () => {
    const edit = presentTool(
      call({
        name: "edit",
        arguments: JSON.stringify({ path: "a.ts", edits: [{ oldText: "a\nb\nc", newText: "a\nB\nB2\nc" }] }),
      }),
    );
    expect(edit.title).toBe("Edit 1 section");
    expect(edit.facts).toEqual([
      { text: "+2", tone: "success" },
      { text: "−1", tone: "danger" },
    ]);
  });

  test("searches with a match count", () => {
    const grep = presentTool(
      call({ name: "grep", arguments: '{"pattern":"lastEventId"}', details: { matchCount: 4 } }),
    );
    expect(grep.chip).toBe("lastEventId in .");
    expect(grep.facts).toEqual([{ text: "4 matches", tone: "muted" }]);
  });

  test("runs a command, reporting a failing exit", () => {
    const bash = presentTool(
      call({ name: "bash", arguments: '{"command":"bun  test\\n src"}', details: { exitCode: 1 } }),
    );
    expect(bash.title).toBe("Run command");
    expect(bash.chip).toBe("bun test src");
    expect(bash.facts).toEqual([{ text: "exit 1", tone: "danger" }]);
    expect(bash.timed).toBe(true);
  });

  test("names a plugin tool by its own name, with the plugin icon", () => {
    expect(presentTool(call({ name: "gdrive__fetch_doc", arguments: '{"url":"https://x"}' }))).toMatchObject({
      title: "Fetch doc",
      icon: "plugin",
    });
  });

  test("shows a built-in tool without a presentation of its own as a tool", () => {
    expect(presentTool(call({ name: "open_url", arguments: '{"url":"https://x"}' }))).toMatchObject({
      title: "Open URL",
      chip: "https://x",
      icon: "tool",
    });
  });
});

test("humanize", () => {
  expect(humanize("fetch_url")).toBe("Fetch url");
});

test("formatDuration", () => {
  expect(formatDuration(14_200)).toBe("0:14");
  expect(formatDuration(125_000)).toBe("2:05");
  expect(formatDuration(3_723_000)).toBe("1:02:03");
});
