import { describe, expect, test } from "bun:test";
import type { Api } from "../api/client";
import type { SessionEventBatch, SessionSnapshot } from "../api/types";
import type { SessionState } from "./projection";
import { acceptBatch, Resync, watchSession } from "./watch";

function batch(sequences: number[], extra: Partial<SessionEventBatch> = {}): SessionEventBatch {
  return {
    streamId: "st",
    events: sequences.map((sequence) => ({
      kind: "usage.changed",
      sessionId: "s1",
      streamId: "st",
      sequence,
      usage: {} as never,
    })),
    ...extra,
  };
}

describe("acceptBatch", () => {
  test("advances over consecutive events and skips ones already applied", () => {
    expect(acceptBatch(batch([4, 5, 6, 7]), "s1", "st", 5)).toBe(7);
  });

  test("resyncs on a gap, another stream, or the server's request", () => {
    expect(() => acceptBatch(batch([7]), "s1", "st", 5)).toThrow(Resync);
    expect(() => acceptBatch(batch([6], { streamId: "other" }), "s1", "st", 5)).toThrow(Resync);
    expect(() => acceptBatch(batch([], { resyncRequired: true }), "s1", "st", 5)).toThrow(Resync);
  });

  test("resyncs on an event of another session", () => {
    expect(() => acceptBatch(batch([6]), "s2", "st", 5)).toThrow(Resync);
  });
});

describe("watchSession", () => {
  test("resume replaces a silent stream with a fresh snapshot and stream", async () => {
    const opened: { after: number; signal: AbortSignal }[] = [];
    let snapshots = 0;
    const api = {
      async getSession(): Promise<SessionSnapshot> {
        snapshots += 1;
        return {
          session: {
            id: "s1",
            configurationRevision: 1,
            createdAt: "",
            updatedAt: "",
            cwd: "/repo",
            model: "m",
          },
          messages: [],
          followUps: { count: 0 },
          eventStreamId: "st",
          eventCursor: 4,
        } as unknown as SessionSnapshot;
      },
      async streamEvents(_id: string, _stream: string, after: number, signal: AbortSignal) {
        opened.push({ after, signal });
        // Open and silent, as a stream held by a suspended page can be.
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            signal.addEventListener("abort", () =>
              controller.error(new DOMException("aborted", "AbortError")),
            );
          },
        });
        return new Response(body);
      },
    } as unknown as Api;
    const connections: string[] = [];
    const watch = watchSession(api, "s1", {
      state() {},
      connection: (connection) => connections.push(connection),
      fatal: (error) => {
        throw error;
      },
    });
    const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
    await settle();
    expect(opened.map((stream) => stream.after)).toEqual([4]);

    watch.resume();
    await settle();

    expect(snapshots).toBe(2);
    expect(opened.map((stream) => stream.after)).toEqual([4, 4]);
    expect(opened.map((stream) => stream.signal.aborted)).toEqual([true, false]);
    expect(connections).toEqual(["connecting", "live", "connecting", "live"]);
    watch.stop();
    expect(opened[1]?.signal.aborted).toBe(true);
  });

  test("reads a live prompt as stored for the attachments its event leaves out", async () => {
    const reads: number[] = [];
    const frame = (events: unknown[]) =>
      `event: session.events\ndata: ${JSON.stringify({ streamId: "st", events })}\n\n`;
    const api = {
      async getSession(): Promise<SessionSnapshot> {
        return {
          session: {
            id: "s1",
            configurationRevision: 1,
            createdAt: "",
            updatedAt: "",
            cwd: "/r",
            model: "m",
          },
          messages: [],
          followUps: { count: 0 },
          eventStreamId: "st",
          eventCursor: 4,
        } as unknown as SessionSnapshot;
      },
      async streamEvents(_id: string, _stream: string, _after: number, signal: AbortSignal) {
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            const base = { sessionId: "s1", streamId: "st" };
            controller.enqueue(
              new TextEncoder().encode(
                frame([
                  { ...base, sequence: 5, kind: "turn.started", turnId: "t1", status: "running" },
                  { ...base, sequence: 6, kind: "user.message.added", turnId: "t1", text: "Look" },
                ]),
              ),
            );
            signal.addEventListener("abort", () =>
              controller.error(new DOMException("aborted", "AbortError")),
            );
          },
        });
        return new Response(body);
      },
      async recentUserMessages(_id: string, limit: number) {
        reads.push(limit);
        return {
          SessionID: "s1",
          HasMore: false,
          Messages: [
            {
              id: "u1",
              role: "user",
              turnId: "t1",
              sequence: 1,
              createdAt: "",
              content: [
                { kind: "text", text: "Look" },
                { kind: "image", attachmentId: "attachment_a", filename: "shot.png", mediaType: "image/png" },
              ],
            },
          ],
        };
      },
    } as unknown as Api;
    let latest: SessionState | undefined;
    const watch = watchSession(api, "s1", {
      state: (state) => {
        latest = state;
      },
      connection() {},
      fatal: (error) => {
        throw error;
      },
    });
    for (let i = 0; i < 10; i += 1) await new Promise((resolve) => setTimeout(resolve, 0));
    watch.stop();
    expect(reads).toEqual([20]);
    expect(latest?.rows).toEqual([
      {
        kind: "user",
        id: "live-user-t1",
        text: "Look",
        attachments: [{ id: "attachment_a", filename: "shot.png", mediaType: "image/png", image: true }],
        comments: [],
      },
    ]);
  });
});
