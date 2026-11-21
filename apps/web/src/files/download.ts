// A file in the session's folder, from one download of it (ADR 0074): its
// bytes up to the view's limit, classified as text or not, and checked
// afterwards against the file's current revision, so a copy that changed
// while it downloaded keeps no revision and takes no comments.
import {
  type Api,
  ApiError,
  fileRevision,
  isAbort,
  NetworkError,
  type WorkspaceFileTarget,
} from "../api/client";

/** The most of a file the view downloads and shows. */
export const MAX_FILE_BYTES = 8 * 1024 * 1024;

/** A file's text as the view shows it. */
export interface FileCopy {
  text: string;
  /**
   * The revision the text downloaded at, which comments anchor to.
   * Undefined for a file without one or a copy that changed while it downloaded.
   */
  revision?: string;
  /** The text stops at the byte limit, before the file's end. */
  limited: boolean;
  /** The file changed while it downloaded, so the text may match no one revision of it. */
  changed: boolean;
}

/** What a load found: text, a file that isn't text, or a copy that's still current. */
export type FileLoad = { kind: "text"; copy: FileCopy } | { kind: "binary" } | { kind: "unchanged" };

/**
 * A response's body, up to limit bytes, and whether there was more. A body
 * past the limit stops downloading there.
 */
export async function readLimited(
  body: ReadableStream<Uint8Array> | null,
  limit: number,
): Promise<{ bytes: Uint8Array; limited: boolean }> {
  if (!body) return { bytes: new Uint8Array(0), limited: false };
  const reader = body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  let limited = false;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      if (size + value.length > limit) {
        chunks.push(value.subarray(0, limit - size));
        size = limit;
        limited = true;
        break;
      }
      chunks.push(value);
      size += value.length;
    }
  } finally {
    if (limited) void reader.cancel().catch(() => {});
    else reader.releaseLock();
  }
  if (chunks.length === 1 && chunks[0]) return { bytes: chunks[0], limited };
  const bytes = new Uint8Array(size);
  let at = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, at);
    at += chunk.length;
  }
  return { bytes, limited };
}

/**
 * The UTF-8 text of downloaded bytes, or undefined when they aren't text: not
 * UTF-8, or holding a NUL. Bytes cut at the limit end at their last line end,
 * or, when the first line doesn't fit, at their last whole character.
 */
export function decodeText(bytes: Uint8Array, limited: boolean): string | undefined {
  let text = bytes;
  if (limited) {
    const newline = text.lastIndexOf(0x0a);
    if (newline >= 0) text = text.subarray(0, newline + 1);
    else {
      // A cut may end inside a character, at most three bytes in.
      for (let drop = 0; drop < 3 && text.length > 0 && !decodes(text); drop++)
        text = text.subarray(0, text.length - 1);
    }
  }
  if (text.includes(0)) return undefined;
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(text);
  } catch {
    return undefined;
  }
}

function decodes(bytes: Uint8Array): boolean {
  try {
    new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    return true;
  } catch {
    return false;
  }
}

/**
 * Downloads a file and says what it is. With `revision`, a file that still
 * has it is `unchanged`. A download is checked afterwards; a file that
 * changed meanwhile downloads once more, and a copy that changed again is
 * kept without a revision.
 */
export async function loadFile(
  api: Api,
  sessionId: string,
  target: WorkspaceFileTarget,
  options: { revision?: string; signal?: AbortSignal; limit?: number } = {},
): Promise<FileLoad> {
  const limit = options.limit ?? MAX_FILE_BYTES;
  let kept: FileLoad | undefined;
  for (let attempt = 0; attempt < 2; attempt++) {
    let response: Response;
    let body: { bytes: Uint8Array; limited: boolean };
    try {
      response = await api.downloadFile(sessionId, target, {
        revision: attempt === 0 ? options.revision : undefined,
        signal: options.signal,
      });
      if (response.status === 304) return { kind: "unchanged" };
      body = await readBody(response, limit);
    } catch (error) {
      // A retry that fails keeps the copy that asked for it.
      if (kept && !isAbort(error)) return kept;
      throw error;
    }
    const text = decodeText(body.bytes, body.limited);
    // A retry that isn't text keeps the copy that asked for it.
    if (text === undefined) return kept ?? { kind: "binary" };
    const revision = fileRevision(response);
    const copy: FileCopy = { text, revision, limited: body.limited, changed: false };
    if (!revision || !(await changed(api, sessionId, target, revision, options.signal)))
      return { kind: "text", copy };
    kept = { kind: "text", copy: { ...copy, revision: undefined, changed: true } };
  }
  return kept ?? { kind: "binary" };
}

async function readBody(response: Response, limit: number): Promise<{ bytes: Uint8Array; limited: boolean }> {
  try {
    return await readLimited(response.body, limit);
  } catch (error) {
    if (isAbort(error)) throw error;
    throw new NetworkError("The file stopped downloading before it was complete.");
  }
}

/**
 * Whether the file no longer has revision: its revision differs, or it or
 * its folder is gone. A check that fails otherwise says nothing.
 */
async function changed(
  api: Api,
  sessionId: string,
  target: WorkspaceFileTarget,
  revision: string,
  signal?: AbortSignal,
): Promise<boolean> {
  try {
    return (await api.fileRevision(sessionId, target, signal)) !== revision;
  } catch (error) {
    if (isAbort(error)) throw error;
    return error instanceof ApiError && (error.status === 404 || error.status === 409);
  }
}
