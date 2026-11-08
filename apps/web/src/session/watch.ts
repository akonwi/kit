// Keeps a session's projection live: a snapshot, then its event stream after
// the snapshot's cursor. A gap, another stream, or the server's resync starts
// over from a fresh snapshot; a dropped connection reconnects with backoff.
import { type Api, ApiError, isAbort } from "../api/client";
import { SSEParser } from "../api/sse";
import type { SessionEventBatch } from "../api/types";
import { Projection, type SessionState, transcript } from "./projection";

/** The stream can't continue from where it is; start over from a snapshot. */
export class Resync extends Error {
  override name = "Resync";
}

/**
 * Checks a batch against the stream and cursor, returning the new cursor.
 * Events at or before the cursor were already applied; a gap or another
 * stream means the client missed events.
 */
export function acceptBatch(
  batch: SessionEventBatch,
  session: string,
  stream: string,
  cursor: number,
): number {
  if (batch.resyncRequired || (batch.streamId !== undefined && batch.streamId !== stream)) {
    throw new Resync("The event stream changed.");
  }
  let next = cursor;
  for (const event of batch.events ?? []) {
    if (event.sessionId !== session || event.streamId !== stream || event.sequence <= 0) {
      throw new Resync("An event doesn't belong to this stream.");
    }
    if (event.sequence <= next) continue;
    if (event.sequence !== next + 1) throw new Resync("Events were missed.");
    next = event.sequence;
  }
  return next;
}

export type Connection = "connecting" | "live" | "reconnecting";

export interface WatchHandlers {
  state(state: SessionState): void;
  connection(connection: Connection): void;
  /** A failure the watch can't recover from by reconnecting. */
  fatal(error: unknown): void;
}

/** Errors that reconnecting won't fix: the credential, the page, or the session. */
function isFatal(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.unauthorized || error.mismatch || error.status === 404 || error.status === 403)
  );
}

export interface Watch {
  /** Reconnects now if the watch is waiting to retry. */
  wake(): void;
  /**
   * Replaces the event stream with a fresh snapshot and stream. A page that
   * was hidden or offline can hold a stream that is open but silent.
   */
  resume(): void;
  /** Reads an older page of the transcript into the projection. */
  loadOlder(): Promise<void>;
  stop(): void;
}

export function watchSession(api: Api, id: string, handlers: WatchHandlers): Watch {
  const controller = new AbortController();
  let wake: (() => void) | undefined;
  let projection: Projection | undefined;
  let failures = 0;
  let attempt: AbortController | undefined;

  const sleep = (ms: number) =>
    new Promise<void>((resolve) => {
      const timer = setTimeout(done, ms);
      function done() {
        clearTimeout(timer);
        wake = undefined;
        resolve();
      }
      wake = done;
      controller.signal.addEventListener("abort", done, { once: true });
    });

  const publish = () => {
    if (projection) handlers.state(projection.session);
  };

  /**
   * Reads a live user message as stored, once, for the attachments its event
   * leaves out. A failure leaves the row to the turn's end, which reads the
   * stored transcript anyway.
   */
  async function fillUserRow(current: Projection, turn: string, signal: AbortSignal): Promise<void> {
    if (!current.session.rows.some((row) => row.id === `live-user-${turn}`)) return;
    try {
      const page = await api.recentUserMessages(id, 20, signal);
      const message = page.Messages.find((value) => value.turnId === turn);
      if (message && projection === current && current.fillUserRow(message)) publish();
    } catch {
      // The turn's end reads the stored transcript.
    }
  }

  async function connect(signal: AbortSignal): Promise<void> {
    let snapshot = await api.getSession(id, signal);
    if (snapshot.session.id !== id) throw new Resync("The server sent another session.");
    projection = new Projection(snapshot, projection?.turnEnd);
    publish();
    const stream = snapshot.eventStreamId ?? "";
    let cursor = snapshot.eventReplayAvailable
      ? (snapshot.eventReplayFrom ?? 0)
      : (snapshot.eventCursor ?? 0);
    let snapshotCursor = snapshot.eventCursor ?? 0;
    const response = await api.streamEvents(id, stream, cursor, signal);
    handlers.connection("live");
    failures = 0;
    const body = response.body;
    if (!body) throw new Resync("The event stream has no body.");
    const reader = body.pipeThrough(new TextDecoderStream()).getReader();
    const parser = new SSEParser();
    try {
      for (;;) {
        const { value, done } = await reader.read();
        if (done) throw new Resync("The event stream ended.");
        for (const frame of parser.push(value)) {
          if (frame.event === "session.resync") throw new Resync("The server asked for a resync.");
          if (frame.event !== "session.events") continue;
          const batch = JSON.parse(frame.data) as SessionEventBatch;
          const updated = acceptBatch(batch, id, stream, cursor);
          if (updated <= cursor) continue;
          let refresh = false;
          const now = Date.now();
          const userTurns: string[] = [];
          for (const event of batch.events ?? []) {
            if (event.sequence <= cursor) continue;
            const live = event.sequence > snapshotCursor;
            projection.apply(event, live ? now : undefined);
            cursor = event.sequence;
            refresh ||= live && (event.kind === "turn.completed" || event.kind === "compaction.completed");
            if (event.kind === "user.message.added" && event.turnId) userTurns.push(event.turnId);
          }
          cursor = updated;
          if (refresh) {
            // The stored transcript replaces the live rows once a turn ends.
            snapshot = await api.getSession(id, signal);
            if (
              snapshot.eventStreamId !== stream ||
              (snapshot.eventCursor ?? 0) < cursor ||
              snapshot.activeTurnId
            ) {
              throw new Resync("Another turn started while refreshing.");
            }
            projection = new Projection(snapshot, projection.turnEnd);
            cursor = snapshot.eventCursor ?? cursor;
            snapshotCursor = cursor;
          } else {
            for (const turn of userTurns) void fillUserRow(projection, turn, signal);
          }
          publish();
        }
      }
    } finally {
      reader.cancel().catch(() => {});
    }
  }

  async function run(): Promise<void> {
    let resyncs: number[] = [];
    handlers.connection("connecting");
    while (!controller.signal.aborted) {
      const current = new AbortController();
      const forward = () => current.abort();
      controller.signal.addEventListener("abort", forward, { once: true });
      attempt = current;
      try {
        await connect(current.signal);
      } catch (error) {
        if (controller.signal.aborted) return;
        // resume() replaced this stream; connect again at once.
        if (current.signal.aborted) continue;
        if (isAbort(error)) return;
        if (isFatal(error)) {
          handlers.fatal(error);
          return;
        }
        if (error instanceof Resync) {
          // Resync promptly, but a stream that keeps resyncing is backed off.
          const now = Date.now();
          resyncs = [...resyncs.filter((at) => now - at < 5000), now];
          if (resyncs.length <= 3) continue;
        }
        failures += 1;
        handlers.connection("reconnecting");
        await sleep(Math.min(15_000, 500 * 2 ** Math.min(failures, 5)));
      } finally {
        controller.signal.removeEventListener("abort", forward);
        if (attempt === current) attempt = undefined;
      }
    }
  }

  void run();

  return {
    wake: () => wake?.(),
    resume() {
      if (controller.signal.aborted) return;
      // Controls wait for the replacement stream instead of acting on a
      // snapshot that may be stale.
      handlers.connection("connecting");
      if (wake) wake();
      else attempt?.abort();
    },
    async loadOlder() {
      const current = projection;
      const cursor = current?.session.previousMessageCursor;
      if (!current || !cursor || !current.session.hasMoreMessages) return;
      const page = await api.transcriptPage(id, cursor, controller.signal);
      if (projection !== current) return;
      current.prepend(transcript(page.messages), page.hasMoreMessages ?? false, page.previousMessageCursor);
      publish();
    },
    stop: () => controller.abort(),
  };
}
