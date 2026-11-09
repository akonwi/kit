// One subagent's conversation, kept live: its stored transcript, with the
// live events the server keeps for it applied on top, as the Apple clients'
// SubagentStreamProjection does. The server keeps a running conversation's
// events in memory only, so the stored transcript is read again after each
// message or tool call completes, and the events it now holds are dropped.
import { type Api, ApiError, isAbort } from "../api/client";
import type {
  SessionEvent,
  SessionSnapshot,
  SubagentLiveEvent,
  SubagentLiveEventPage,
  SubagentTranscript,
  TranscriptMessage,
} from "../api/types";
import { InvalidPayload, Projection, type SessionState } from "../session/projection";

/** At most this many events come in one page. */
const PAGE_EVENTS = 128;
/** The stored transcript is read at least this often while events arrive. */
const HISTORY_MS = 3_000;
/** How soon to ask again while the agent works. */
const ACTIVE_MS = 250;
/** How soon to ask again while it's idle. */
const IDLE_MS = 2_000;
/** Starting over this many times in a row is a fault, not a race. */
const MAX_RESETS = 3;

const BOUNDARIES = new Set(["message.completed", "tool.started", "tool.completed", "turn.settled"]);

/**
 * A tool call's id within its turn. A conversation's provider may reuse
 * call ids across turns, so they are told apart by turn.
 */
export function scopedToolId(turn: string, call: string): string {
  return `${turn.length}:${turn}${call}`;
}

function scoped(message: TranscriptMessage): TranscriptMessage {
  const turn = message.turnId;
  return {
    ...message,
    toolCallId: message.toolCallId === undefined ? undefined : scopedToolId(turn, message.toolCallId),
    content: message.content?.map((block) =>
      block.kind === "toolCall" ? { ...block, toolCallId: scopedToolId(turn, block.toolCallId) } : block,
    ),
  };
}

/** A subagent event as the session projection reads it, or nothing it shows. */
export function sessionEvent(
  event: SubagentLiveEvent,
  conversation: string,
  stream: string,
): SessionEvent | undefined {
  const base = { sessionId: conversation, streamId: stream, sequence: event.sequence };
  switch (event.kind) {
    case "message.text.delta":
      return {
        ...base,
        kind: "assistant.text.delta",
        turnId: event.turnId,
        messageId: event.messageId,
        contentIndex: event.contentIndex,
        delta: event.delta,
      };
    case "message.thinking.delta":
      return {
        ...base,
        kind: "assistant.thinking.delta",
        turnId: event.turnId,
        messageId: event.messageId,
        contentIndex: event.contentIndex,
        delta: event.delta,
      };
    case "message.completed":
      return event.messageId
        ? { ...base, kind: "assistant.completed", turnId: event.turnId, messageId: event.messageId }
        : undefined;
    case "turn.started":
      return { ...base, kind: "turn.started", turnId: event.turnId, status: "running" };
    case "turn.settled":
      return { ...base, kind: "turn.completed", turnId: event.turnId, status: "completed" };
    case "tool.planned":
      return {
        ...base,
        kind: "tool.planned",
        turnId: event.turnId,
        messageId: "",
        contentIndex: event.contentIndex,
        toolCallId: scopedToolId(event.turnId ?? "", event.toolCallId),
        toolName: event.toolName,
      };
    case "tool.started":
      return {
        ...base,
        kind: "tool.started",
        turnId: event.turnId,
        toolCallId: scopedToolId(event.turnId ?? "", event.toolCallId),
        toolName: event.toolName,
      };
    case "tool.output.delta":
      return {
        ...base,
        kind: "tool.output.delta",
        turnId: event.turnId,
        toolCallId: scopedToolId(event.turnId ?? "", event.toolCallId),
        toolName: event.toolName,
        content: [{ kind: "text", text: event.text ?? "" }],
        isError: event.isError,
      };
    case "tool.completed":
      return {
        ...base,
        kind: "tool.completed",
        turnId: event.turnId,
        toolCallId: scopedToolId(event.turnId ?? "", event.toolCallId),
        toolName: event.toolName,
        content: event.text === undefined ? undefined : [{ kind: "text", text: event.text }],
        isError: event.isError,
      };
    case "boundary.created":
      return {
        ...base,
        kind: "boundary.created",
        boundaryId: event.boundaryId,
        boundaryKind: event.boundaryKind,
        boundarySource: event.boundarySource,
        content: event.text ? [{ kind: "text", text: event.text }] : undefined,
        details: event.details,
        createdAt: event.createdAt,
      };
    default:
      return undefined;
  }
}

/** Checks one event's fields against its kind, as the contract states them. */
function checkEvent(event: SubagentLiveEvent): void {
  const fields = event as Partial<Record<string, unknown>>;
  const bad = () => new InvalidPayload(`A subagent event (${event.kind}) is malformed.`);
  if ((fields.contentIndex as number | undefined) !== undefined && (fields.contentIndex as number) < 0)
    throw bad();
  switch (event.kind) {
    case "message.text.delta":
    case "message.thinking.delta":
      if (!event.messageId || !event.delta) throw bad();
      break;
    case "tool.planned":
    case "tool.started":
    case "tool.output.delta":
    case "tool.completed":
      if (!event.toolCallId || !event.toolName) throw bad();
      break;
    case "boundary.created":
      if (!event.boundaryId || !event.boundaryKind || !event.createdAt) throw bad();
      break;
    case "message.completed":
    case "turn.started":
    case "turn.settled":
      break;
    default:
      // A kind added later changes nothing this client shows.
      break;
  }
}

/** What a page of live events came to. */
export type Accepted =
  | { reset: true }
  | {
      reset: false;
      /** How many events were new. */
      added: number;
      /** Whether a message or tool call completed, so the stored transcript has more. */
      boundary: boolean;
      /** Whether this was the first page of a stream. */
      first: boolean;
    };

/** One subagent conversation's transcript and live events, with no I/O. */
export class ChildConversation {
  private messages: TranscriptMessage[] = [];
  private hasMore = false;
  private previousCursor: string | undefined;
  /** The live stream this has events from, and the last one taken. */
  stream = "";
  cursor = 0;
  /** Events not yet in the stored transcript, oldest first. */
  private events: SubagentLiveEvent[] = [];
  /** When each live event arrived; undefined for ones replayed on joining a stream. */
  private readonly received = new Map<number, number>();
  private projection: Projection | undefined;
  private applied = 0;

  constructor(readonly id: string) {}

  /** The conversation as the transcript shows it, once its history is read. */
  get state(): SessionState | undefined {
    return this.projection?.session;
  }

  /** Forgets the live stream; the next page starts a new one. */
  reset(): void {
    this.stream = "";
    this.cursor = 0;
    this.events = [];
    this.received.clear();
  }

  /** Takes the newest page of the stored transcript, keeping older pages read before it. */
  history(page: SubagentTranscript): void {
    const newest = this.check(page);
    const first = newest[0]?.sequence;
    const prefix =
      first === undefined || this.messages.length === 0
        ? []
        : this.messages.filter((message) => message.sequence < first);
    // Older pages stay only when the newest one meets them; a gap reads again.
    const meets = first !== undefined && this.messages.some((message) => message.sequence >= first);
    if (prefix.length > 0 && meets) {
      this.messages = [...prefix, ...newest];
    } else {
      this.messages = newest;
      this.hasMore = page.hasMoreMessages ?? false;
      this.previousCursor = page.previousMessageCursor;
    }
    this.rebuild();
  }

  /** Puts an older page of the stored transcript before the messages read. */
  older(page: SubagentTranscript): void {
    const older = this.check(page);
    const first = this.messages[0]?.sequence;
    if (first !== undefined && older.some((message) => message.sequence >= first)) {
      throw new InvalidPayload("An older page of the subagent's transcript overlaps the newer one.");
    }
    this.messages = [...older, ...this.messages];
    this.hasMore = page.hasMoreMessages ?? false;
    this.previousCursor = page.previousMessageCursor;
    this.rebuild();
  }

  /** The cursor for the page before the oldest message read, if there is one. */
  get before(): string | undefined {
    return this.hasMore ? this.previousCursor : undefined;
  }

  /**
   * Takes a page of live events that arrived at `now`. A page from another
   * stream, one the server says to start over from, or one that skips
   * events resets the stream, and the stored transcript is read again.
   */
  accept(page: SubagentLiveEventPage, now: number): Accepted {
    const first = page.firstSequence ?? 0;
    const last = page.lastSequence ?? 0;
    const incoming = page.events ?? [];
    if (
      first < 0 ||
      last < first ||
      (first === 0) !== (last === 0) ||
      incoming.length > PAGE_EVENTS ||
      (first > 0 && !page.streamId)
    ) {
      throw new InvalidPayload("A page of subagent events is malformed.");
    }
    if (page.resyncRequired && incoming.length > 0) {
      throw new InvalidPayload("A page that asks to start over carries events.");
    }
    if (page.resyncRequired || (this.stream && page.streamId !== this.stream)) {
      this.reset();
      return { reset: true };
    }
    const joining = !this.stream;
    let previous: number | undefined;
    let added = 0;
    let boundary = false;
    for (const event of incoming) {
      if (
        event.sequence <= 0 ||
        event.sequence < first ||
        event.sequence > last ||
        (previous !== undefined && event.sequence !== previous + 1)
      ) {
        throw new InvalidPayload("Subagent events are out of order.");
      }
      checkEvent(event);
      previous = event.sequence;
      if (event.sequence <= this.cursor) continue;
      if (this.cursor !== 0 && event.sequence !== this.cursor + 1) {
        this.reset();
        return { reset: true };
      }
      this.events.push(event);
      if (!joining) this.received.set(event.sequence, now);
      this.cursor = event.sequence;
      added += 1;
      boundary ||= BOUNDARIES.has(event.kind);
    }
    if (last !== 0 && this.cursor !== last) {
      this.reset();
      return { reset: true };
    }
    this.stream = page.streamId ?? "";
    return { reset: false, added, boundary, first: joining };
  }

  /** Applies the events taken since the last call. */
  catchUp(): void {
    if (!this.projection) return;
    for (const event of this.events) {
      if (event.sequence <= this.applied) continue;
      const mapped = sessionEvent(event, this.id, this.stream);
      if (mapped) this.projection.apply(mapped, this.received.get(event.sequence));
      this.applied = event.sequence;
    }
  }

  private check(page: SubagentTranscript): TranscriptMessage[] {
    if (page.conversationId !== this.id) {
      throw new InvalidPayload("The server sent another subagent's transcript.");
    }
    let previous = -1;
    for (const message of page.messages) {
      if (!message.id || !message.turnId || message.sequence <= previous) {
        throw new InvalidPayload("A subagent transcript message is malformed.");
      }
      previous = message.sequence;
    }
    return page.messages.map(scoped);
  }

  /** The turn the kept events show starting and not yet settled. */
  private activeTurn(): string | undefined {
    let active: string | undefined;
    for (const event of this.events) {
      if (event.kind === "turn.started") active = event.turnId;
      else if (event.kind === "turn.settled" && event.turnId === active) active = undefined;
    }
    return active;
  }

  /** Projects the stored transcript, then every kept event on top of it. */
  private rebuild(): void {
    // A settled turn the transcript holds needs none of its events.
    const stored = new Set(
      this.messages.filter((m) => m.role === "assistant" && m.stopReason).map((m) => m.turnId),
    );
    const settled = this.events.findLastIndex(
      (event) => event.kind === "turn.settled" && stored.has(event.turnId ?? ""),
    );
    if (settled >= 0) {
      for (const event of this.events.slice(0, settled + 1)) this.received.delete(event.sequence);
      this.events = this.events.slice(settled + 1);
    }
    this.projection = new Projection(this.snapshot(this.activeTurn()));
    this.applied = 0;
    this.catchUp();
  }

  /** The stored transcript as a session snapshot, which is all the projection reads. */
  private snapshot(active: string | undefined): SessionSnapshot {
    const zero = { cacheRead: 0, cacheWrite: 0, input: 0, output: 0 };
    return {
      session: {
        id: this.id,
        configurationRevision: 0,
        createdAt: "",
        updatedAt: "",
        cwd: "",
        model: "",
        thinkingLevel: "off",
      },
      messages: this.messages,
      activeTurnId: active,
      eventReplayAvailable: true,
      hasMoreMessages: this.hasMore,
      previousMessageCursor: this.previousCursor,
      followUps: { count: 0 },
      usage: { ...zero, reasoning: 0, totalTokens: 0, cost: { ...zero, total: 0 } },
    };
  }
}

/** What the subagent's view shows. */
export interface ChildView {
  /** The conversation, once its history is read. */
  state?: SessionState;
  /** Why it isn't shown or kept live. */
  error?: string;
  /** The conversation is gone; trying again won't bring it back. */
  unavailable: boolean;
  /** Reading stopped on the error; only trying again starts it. */
  halted?: boolean;
}

export interface ChildWatch {
  /** Whether the conversation is in view; out of view, it isn't asked about. */
  setActive(active: boolean): void;
  /** Asks again now, such as when the roster says the agent changed. */
  wake(): void;
  /** Reads an older page of the conversation. */
  loadOlder(): Promise<void>;
  /** Starts again after a failure it stopped on. */
  retry(): void;
  stop(): void;
}

export interface ChildHandlers {
  view(view: ChildView): void;
  /** Hands a failure to the app first; true when the app took it, such as a lost credential. */
  fail(error: unknown): boolean;
}

/** A failure that asking again won't fix. */
function lasting(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 404 || error.status === 410);
}

/** Keeps a subagent's conversation live while it's in view and the page shows. */
export function watchChild(
  api: Api,
  session: string,
  conversationId: string,
  handlers: ChildHandlers,
): ChildWatch {
  const controller = new AbortController();
  const signal = controller.signal;
  const conversation = new ChildConversation(conversationId);
  let active = false;
  let wakeUp: (() => void) | undefined;
  let historyAt = 0;
  let error: string | undefined;
  let unavailable = false;
  let halted = false;
  let running = false;

  const publish = () => handlers.view({ state: conversation.state, error, unavailable, halted });

  /** Waits `ms`, or until woken, or until stopped. */
  const pause = (ms: number) =>
    new Promise<void>((resolve) => {
      const timer = ms === Number.POSITIVE_INFINITY ? undefined : setTimeout(done, ms);
      function done() {
        clearTimeout(timer);
        wakeUp = undefined;
        signal.removeEventListener("abort", done);
        resolve();
      }
      wakeUp = done;
      signal.addEventListener("abort", done, { once: true });
    });

  const showing = () => active && document.visibilityState !== "hidden";
  const visibility = () => {
    if (showing()) wakeUp?.();
  };
  document.addEventListener("visibilitychange", visibility);

  async function readHistory(): Promise<void> {
    const page = await api.subagentTranscript(session, conversationId, undefined, signal);
    conversation.history(page);
    historyAt = Date.now();
  }

  async function run(): Promise<void> {
    running = true;
    let failures = 0;
    let resets = 0;
    try {
      while (!signal.aborted) {
        if (!showing()) {
          await pause(Number.POSITIVE_INFINITY);
          continue;
        }
        try {
          if (!conversation.state) {
            await readHistory();
            publish();
          }
          let page: SubagentLiveEventPage | undefined;
          try {
            page = await api.subagentEvents(
              session,
              conversationId,
              conversation.stream,
              conversation.cursor,
              signal,
            );
          } catch (failure) {
            // The server keeps events only for conversations it runs; the
            // stored transcript, read below, still says whether this one exists.
            if (!(failure instanceof ApiError && failure.status === 404)) throw failure;
          }
          const now = Date.now();
          let refresh = now - historyAt >= HISTORY_MS;
          let busy = false;
          if (!page) {
            // The stored transcript replaces what the discarded stream showed.
            conversation.reset();
            refresh = true;
          } else {
            const accepted = conversation.accept(page, now);
            if (accepted.reset) {
              resets += 1;
              if (resets > MAX_RESETS) throw new InvalidPayload("The subagent's events keep starting over.");
              refresh = true;
            } else {
              resets = 0;
              busy = accepted.added > 0;
              refresh ||= accepted.boundary || (accepted.first && accepted.added > 0);
            }
          }
          // History after a completion, so what was stored wins over replayed events.
          if (refresh) await readHistory();
          else conversation.catchUp();
          failures = 0;
          error = undefined;
          publish();
          await pause(busy || conversation.state?.activeTurnId ? ACTIVE_MS : IDLE_MS);
        } catch (failure) {
          if (signal.aborted || isAbort(failure)) return;
          if (handlers.fail(failure)) return;
          if (lasting(failure)) {
            unavailable = true;
            error = "This subagent conversation is no longer available.";
            publish();
            return;
          }
          if (failure instanceof InvalidPayload) {
            error = failure.message;
            halted = true;
            publish();
            return;
          }
          failures += 1;
          const delay = Math.min(15, 2 ** (failures - 1));
          const reason = failure instanceof Error ? failure.message : "The conversation couldn’t be read.";
          error = `${reason} Retrying in ${delay}s…`;
          publish();
          await pause(delay * 1000);
        }
      }
    } finally {
      running = false;
    }
  }

  void run();

  return {
    setActive(value) {
      active = value;
      if (showing()) wakeUp?.();
    },
    wake: () => wakeUp?.(),
    async loadOlder() {
      const before = conversation.before;
      if (!before) return;
      const page = await api.subagentTranscript(session, conversationId, before, signal);
      conversation.older(page);
      publish();
    },
    retry() {
      if (running || signal.aborted || unavailable) return;
      error = undefined;
      halted = false;
      conversation.reset();
      publish();
      void run();
    },
    stop() {
      controller.abort();
      document.removeEventListener("visibilitychange", visibility);
    },
  };
}
