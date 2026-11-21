import { describe, expect, test } from "bun:test";
import { Api, ApiError } from "../api/client";
import { decodeText, loadFile, readLimited } from "./download";

const encode = (text: string) => new TextEncoder().encode(text);

function stream(...chunks: string[]): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encode(chunk));
      controller.close();
    },
  });
}

describe("readLimited", () => {
  test("reads a body that fits whole", async () => {
    const { bytes, limited } = await readLimited(stream("ab", "cd"), 4);
    expect(new TextDecoder().decode(bytes)).toBe("abcd");
    expect(limited).toBe(false);
  });

  test("stops at the limit and says there was more", async () => {
    const { bytes, limited } = await readLimited(stream("ab", "cd", "ef"), 3);
    expect(new TextDecoder().decode(bytes)).toBe("abc");
    expect(limited).toBe(true);
  });

  test("reads no body as empty", async () => {
    expect(await readLimited(null, 3)).toEqual({ bytes: new Uint8Array(0), limited: false });
  });
});

describe("decodeText", () => {
  test("decodes UTF-8 whole", () => {
    expect(decodeText(encode("héllo\nwörld"), false)).toBe("héllo\nwörld");
  });

  test("calls bytes with a NUL or invalid UTF-8 not text", () => {
    expect(decodeText(encode("a\u0000b"), false)).toBeUndefined();
    expect(decodeText(new Uint8Array([0x61, 0xff, 0x62]), false)).toBeUndefined();
  });

  test("cuts a limited copy at its last line end", () => {
    expect(decodeText(encode("one\ntwo\nthr"), true)).toBe("one\ntwo\n");
  });

  test("cuts a first line that doesn't fit at its last whole character", () => {
    // "é" is two bytes; the cut falls between them.
    const bytes = encode("abé").subarray(0, 3);
    expect(decodeText(bytes, true)).toBe("ab");
  });
});

interface Answer {
  status: number;
  body?: string;
  headers?: Record<string, string>;
}

/** An Api over a server that answers requests in turn, and the requests it got. */
function server(...answers: Answer[]) {
  const requests: { method: string; url: string; headers: Record<string, string> }[] = [];
  const api = new Api("token", async (url, init) => {
    requests.push({
      method: init?.method ?? "GET",
      url,
      headers: (init?.headers ?? {}) as Record<string, string>,
    });
    const answer = answers.shift();
    if (!answer) throw new Error(`unexpected request ${url}`);
    return new Response(answer.body ?? null, { status: answer.status, headers: answer.headers });
  });
  return { api, requests };
}

const target = { workspaceId: "ws_1", path: "src/a b.ts" };
const tagged = (revision: string) => ({ ETag: `"${revision}"` });

describe("loadFile", () => {
  test("downloads text and checks it afterwards", async () => {
    const { api, requests } = server(
      { status: 200, body: "let a = 1;\n", headers: tagged("file_1") },
      { status: 200, headers: tagged("file_1") },
    );
    expect(await loadFile(api, "s1", target)).toEqual({
      kind: "text",
      copy: { text: "let a = 1;\n", revision: "file_1", limited: false, changed: false },
    });
    expect(requests.map((request) => request.method)).toEqual(["GET", "HEAD"]);
    expect(requests[0]?.url).toBe("/v1/sessions/s1/files/content?workspaceId=ws_1&path=src%2Fa+b.ts");
    expect(requests[0]?.headers.Accept).toBe("*/*");
    expect(requests[0]?.headers["If-None-Match"]).toBeUndefined();
  });

  test("keeps a copy whose revision is still current", async () => {
    const { api, requests } = server({ status: 304, headers: tagged("file_1") });
    expect(await loadFile(api, "s1", target, { revision: "file_1" })).toEqual({ kind: "unchanged" });
    expect(requests[0]?.headers["If-None-Match"]).toBe('"file_1"');
  });

  test("downloads again once a file changed while it downloaded", async () => {
    const { api } = server(
      { status: 200, body: "old\n", headers: tagged("file_1") },
      { status: 200, headers: tagged("file_2") },
      { status: 200, body: "new\n", headers: tagged("file_2") },
      { status: 200, headers: tagged("file_2") },
    );
    expect(await loadFile(api, "s1", target)).toEqual({
      kind: "text",
      copy: { text: "new\n", revision: "file_2", limited: false, changed: false },
    });
  });

  test("keeps a copy that changed again without a revision", async () => {
    const { api } = server(
      { status: 200, body: "one\n", headers: tagged("file_1") },
      { status: 200, headers: tagged("file_2") },
      { status: 200, body: "two\n", headers: tagged("file_2") },
      { status: 404 },
    );
    expect(await loadFile(api, "s1", target)).toEqual({
      kind: "text",
      copy: { text: "two\n", revision: undefined, limited: false, changed: true },
    });
  });

  test("keeps the changed copy when the retry fails", async () => {
    const { api } = server(
      { status: 200, body: "one\n", headers: tagged("file_1") },
      { status: 409 },
      { status: 503, body: JSON.stringify({ error: { code: "unavailable", message: "down" } }) },
    );
    expect(await loadFile(api, "s1", target)).toEqual({
      kind: "text",
      copy: { text: "one\n", revision: undefined, limited: false, changed: true },
    });
  });

  test("trusts a copy when the check fails for another reason", async () => {
    const { api } = server({ status: 200, body: "one\n", headers: tagged("file_1") }, { status: 503 });
    expect(await loadFile(api, "s1", target)).toMatchObject({ kind: "text", copy: { revision: "file_1" } });
  });

  test("shows a file with no revision without checking it", async () => {
    const { api, requests } = server({ status: 200, body: "cpu 1\n" });
    expect(await loadFile(api, "s1", target)).toEqual({
      kind: "text",
      copy: { text: "cpu 1\n", revision: undefined, limited: false, changed: false },
    });
    expect(requests).toHaveLength(1);
  });

  test("cuts a long file at the limit", async () => {
    const { api } = server(
      { status: 200, body: "aa\nbb\ncc\n", headers: tagged("file_1") },
      { status: 200, headers: tagged("file_1") },
    );
    expect(await loadFile(api, "s1", target, { limit: 7 })).toEqual({
      kind: "text",
      copy: { text: "aa\nbb\n", revision: "file_1", limited: true, changed: false },
    });
  });

  test("calls a file that isn't text binary", async () => {
    const { api } = server({ status: 200, body: "\u0000PNG", headers: tagged("file_1") });
    expect(await loadFile(api, "s1", target)).toEqual({ kind: "binary" });
  });

  test("fails with the server's error", async () => {
    const { api } = server({
      status: 404,
      body: JSON.stringify({ error: { code: "not_found", message: "missing" } }),
    });
    const failure = await loadFile(api, "s1", target).catch((error) => error);
    expect(failure).toBeInstanceOf(ApiError);
    expect(failure.code).toBe("not_found");
  });

  test("fails as an older server's 404 when the route is missing", async () => {
    const { api } = server({ status: 404, body: "404 page not found" });
    const failure = await loadFile(api, "s1", target).catch((error) => error);
    expect(failure.code).toBe("http_404");
  });
});
