import { describe, expect, test } from "bun:test";
import { SSEParser } from "./sse";

describe("SSEParser", () => {
  test("splits frames across chunks", () => {
    const parser = new SSEParser();
    expect(parser.push('event: session.events\ndata: {"a"')).toEqual([]);
    expect(parser.push(":1}\nid: 7\n\nevent: session.resync\ndata: {}\n\n")).toEqual([
      { event: "session.events", data: '{"a":1}', id: "7" },
      { event: "session.resync", data: "{}", id: "7" },
    ]);
  });

  test("joins data lines, ignores comments, and accepts CRLF", () => {
    const parser = new SSEParser();
    expect(parser.push(": keepalive\r\ndata: one\r\ndata:two\r\n\r\n")).toEqual([
      { event: "message", data: "one\ntwo", id: undefined },
    ]);
  });

  test("rejects a record larger than the limit", () => {
    const parser = new SSEParser();
    expect(() => parser.push(`data: ${"x".repeat(2 * 1024 * 1024 + 1)}`)).toThrow("too large");
  });
});
