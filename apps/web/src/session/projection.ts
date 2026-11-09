// Projects a session snapshot and its live events into transcript rows, as
// the Apple clients' SessionProjection and SessionEventProjection do. Rows
// are never changed in place once emitted, so the view can key them by
// identity.
import type {
  Annotation,
  FollowUpQueue,
  InteractionRequest,
  Scratchpad,
  SessionEvent,
  SessionInfo,
  SessionSnapshot,
  SessionUsage,
  SubagentConversation,
  SubmittedAnnotation,
  TranscriptContent,
  TranscriptMessage,
  WorkspaceRef,
} from "../api/types";
import { type Comment, fromAnnotation, fromSummary, mergeComments } from "../changes/comments";
import { newerScratchpad } from "../scratchpad/pad";
import { type ShellRun, shellFromBoundary } from "./shell";

export type ToolStatus = "planned" | "running" | "done" | "notRun";

export interface ToolCall {
  id: string;
  name: string;
  arguments?: string;
  output: string;
  /** Files the result carries, such as the image show_image shows. */
  attachments: Attachment[];
  failed: boolean;
  status: ToolStatus;
  details?: Record<string, unknown>;
  /** When a call seen starting live started, in epoch milliseconds. */
  startedAt?: number;
  finishedAt?: number;
}

export interface Attachment {
  id?: string;
  filename: string;
  mediaType: string;
  image: boolean;
}

export type TurnEnd = "failed" | "stopped" | "interrupted";

export type Row =
  | { kind: "user"; id: string; text: string; attachments: Attachment[]; comments: SubmittedAnnotation[] }
  | { kind: "assistant"; id: string; text: string; attachments: Attachment[] }
  | { kind: "tools"; id: string; tools: ToolCall[] }
  | { kind: "plugin"; id: string; text: string; plugin: string }
  | { kind: "compaction"; id: string; summary: string }
  | { kind: "cwd"; id: string; path: string }
  | { kind: "shell"; id: string; run: ShellRun }
  | { kind: "turnEnd"; id: string; end: TurnEnd; message: string };

export type Status = "idle" | "running" | "awaitingResponse";

export interface SessionState {
  info: SessionInfo;
  rows: Row[];
  status: Status;
  activeTurnId?: string;
  /** The shell command running when the snapshot was taken, from any device. */
  activeShellId?: string;
  /** A fixed activity label or the latest line of streamed thinking. */
  activity?: string;
  compacting: boolean;
  pendingInteractions: InteractionRequest[];
  followUps: FollowUpQueue;
  contextTokens?: number;
  contextWindow?: number;
  usage: SessionUsage;
  retryAt?: string;
  /** Which attempt the scheduled provider retry will be. */
  retryCount?: number;
  hasMoreMessages: boolean;
  previousMessageCursor?: string;
  /** The session's folder, which its changes are read from. */
  workspace?: WorkspaceRef;
  /** Comments waiting to go with the next message, oldest first. */
  comments: Comment[];
  /** The newest copy of the scratchpad the session shares with its forks. */
  scratchpad?: Scratchpad;
  /** The subagents' conversations, as the latest snapshot listed them. */
  subagents: SubagentConversation[];
}

export const WORKING = "Working…";
export const THINKING = "Thinking…";
export const COMPACTING = "Compacting session…";

const TURN_END = "turn-end:";
const COMPACTION_PREFIX = "[context summary]";

/** A wire record that breaks the contract's rules; the watch starts over. */
export class InvalidPayload extends Error {
  override name = "InvalidPayload";
}

/** Text a person reads in content: text blocks and prompt-command invocations. */
export function visibleText(content: readonly TranscriptContent[]): string {
  const parts: string[] = [];
  for (const block of content) {
    if (block.kind === "text" && block.text) parts.push(block.text);
    if (block.kind === "promptCommand") {
      const invocation = `/${block.name}${block.arguments ? ` ${block.arguments}` : ""}`;
      parts.push(invocation);
    }
  }
  return parts.join("\n\n");
}

export function attachments(content: readonly TranscriptContent[]): Attachment[] {
  const result: Attachment[] = [];
  for (const block of content) {
    if (block.kind === "image" || block.kind === "file") {
      result.push({
        id: block.attachmentId,
        filename: block.filename ?? "Attachment",
        mediaType: block.mediaType,
        image: block.kind === "image",
      });
    }
  }
  return result;
}

/** The comments a sent message carried. */
export function sentComments(content: readonly TranscriptContent[]): SubmittedAnnotation[] {
  return content.flatMap((block) => (block.kind === "annotations" ? block.annotations : []));
}

function object(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

/** The row for a context message the user should notice, or undefined. */
export function contextRow(message: {
  id: string;
  boundaryId?: string;
  boundaryKind?: string;
  boundarySource?: string;
  content?: TranscriptContent[];
  details?: unknown;
}): Row | undefined {
  const id = message.boundaryId || message.id;
  switch (message.boundaryKind) {
    case "summary": {
      let summary = visibleText(message.content ?? []);
      if (summary.startsWith(COMPACTION_PREFIX)) summary = summary.slice(COMPACTION_PREFIX.length);
      summary = summary.trim();
      return summary ? { kind: "compaction", id, summary } : undefined;
    }
    case "bash": {
      const run = shellFromBoundary(id, message.details, message.content ?? [], message.boundarySource);
      return run ? { kind: "shell", id, run } : undefined;
    }
    case "cwd": {
      const details = object(message.details);
      const cwd = details?.cwd;
      return details?.version === 1 && typeof cwd === "string" && cwd
        ? { kind: "cwd", id, path: cwd }
        : undefined;
    }
    default:
      return undefined;
  }
}

export function turnEndRow(turn: string, end: TurnEnd, message: string): Row {
  return { kind: "turnEnd", id: TURN_END + turn, end, message };
}

/**
 * Projects stored messages into rows. `settled` names turns whose calls
 * without a result never ran; `finished` names turns that are over, which
 * close with a row when they didn't complete. `carried` holds turn-end rows
 * the live stream showed, which win over the stored reason. A call without a
 * result in the `active` turn is still running; history pages pass none,
 * since a call's result may lie in a later page.
 */
export function transcript(
  source: readonly TranscriptMessage[],
  settled: (turn: string) => boolean = () => false,
  finished: (turn: string) => boolean = () => false,
  carried: readonly Row[] = [],
  active?: string,
): Row[] {
  const rows: Row[] = [];
  const seen = new Set<string>();
  const calls = new Set<string>();
  const results = new Map<string, TranscriptMessage>();
  for (const message of source) {
    if (
      !message.id ||
      seen.has(message.id) ||
      !["user", "assistant", "tool", "context"].includes(message.role)
    ) {
      throw new InvalidPayload(`Transcript message ${message.id || "(no id)"} is malformed.`);
    }
    seen.add(message.id);
    for (const block of message.content ?? []) {
      if (block.kind === "toolCall") calls.add(block.toolCallId);
    }
    if (message.role === "tool" && message.toolCallId) results.set(message.toolCallId, message);
  }
  const lastIndex = new Map<string, number>();
  const lastReply = new Map<string, TranscriptMessage>();
  source.forEach((message, index) => {
    if (!message.turnId) return;
    lastIndex.set(message.turnId, index);
    if (message.role === "assistant") lastReply.set(message.turnId, message);
  });
  const carriedByTurn = new Map<string, Row>();
  for (const row of carried) {
    if (row.kind === "turnEnd") carriedByTurn.set(row.id.slice(TURN_END.length), row);
  }

  let pending: ToolCall[] = [];
  let turn: string | undefined;
  const flush = () => {
    const first = pending[0];
    if (!first) return;
    rows.push({ kind: "tools", id: `tools-${first.id}`, tools: pending });
    pending = [];
  };
  const endTurn = (id: string) => {
    if (!finished(id)) return;
    let row = carriedByTurn.get(id);
    carriedByTurn.delete(id);
    if (!row) {
      const reply = lastReply.get(id);
      const error = reply?.errorMessage?.trim();
      if (!reply || !error) return;
      row = turnEndRow(id, reply.stopReason === "aborted" ? "stopped" : "failed", error);
    }
    flush();
    rows.push(row);
  };

  source.forEach((message, index) => {
    project(message);
    if (message.turnId && lastIndex.get(message.turnId) === index) endTurn(message.turnId);
  });

  function project(message: TranscriptMessage) {
    // Results enrich the original call; they make no row of their own.
    if (message.role === "tool" && message.toolCallId && calls.has(message.toolCallId)) return;
    if (turn !== message.turnId) {
      flush();
      turn = message.turnId;
    }
    if (message.role === "context") {
      flush();
      if (message.boundaryKind === "plugin_message") {
        if (!message.boundarySource) throw new InvalidPayload("A plugin message names no plugin.");
        rows.push({
          kind: "plugin",
          id: message.id,
          text: visibleText(message.content ?? []),
          plugin: message.boundarySource,
        });
        return;
      }
      const row = contextRow(message);
      if (row) rows.push(row);
      return;
    }
    if (message.role === "tool") {
      if (!message.toolCallId) return;
      pending.push({
        id: message.toolCallId,
        name: message.toolName ?? "Tool",
        output: visibleText(message.content ?? []),
        attachments: attachments(message.content ?? []),
        failed: message.isError ?? false,
        status: "done",
        details: object(message.details),
      });
      return;
    }
    if (message.role !== "assistant") flush();
    let visible: TranscriptContent[] = [];
    let segment = 0;
    const flushText = () => {
      const text = visibleText(visible);
      const media = attachments(visible);
      const comments = message.role === "user" ? sentComments(visible) : [];
      visible = [];
      if (!text && media.length === 0 && comments.length === 0) return;
      flush();
      const id = segment === 0 ? message.id : `${message.id}-text-${segment}`;
      rows.push(
        message.role === "user"
          ? { kind: "user", id, text, attachments: media, comments }
          : { kind: "assistant", id, text, attachments: media },
      );
      segment += 1;
    };
    for (const block of message.content ?? []) {
      if (block.kind === "toolCall") {
        flushText();
        const result = results.get(block.toolCallId);
        pending.push({
          id: block.toolCallId,
          name: block.toolName,
          arguments: block.arguments,
          output: visibleText(result?.content ?? []),
          attachments: attachments(result?.content ?? []),
          failed: result?.isError ?? false,
          status: result
            ? "done"
            : settled(message.turnId)
              ? "notRun"
              : message.turnId === active
                ? "running"
                : "done",
          details: object(result?.details),
        });
      } else if (block.kind !== "thinking") {
        visible.push(block);
      }
    }
    flushText();
  }

  flush();
  // A live row whose turn stored nothing still closes the transcript.
  for (const [id, row] of [...carriedByTurn].sort(([a], [b]) => a.localeCompare(b))) {
    if (finished(id)) rows.push(row);
  }
  return rows;
}

function joined(blocks: Map<number, string> | undefined): string {
  if (!blocks) return "";
  return [...blocks.entries()]
    .sort(([a], [b]) => a - b)
    .map(([, value]) => value)
    .join("");
}

/** The last non-blank line of streamed thinking. */
export function latestLine(thinking: string): string | undefined {
  const lines = thinking
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
  return lines.at(-1);
}

/** Ordered live presentation layered on the last authoritative snapshot. */
export class Projection {
  private state: SessionState;
  private readonly text = new Map<string, Map<number, string>>();
  private readonly thinking = new Map<string, Map<number, string>>();
  private readonly userTurns: Set<string>;
  private readonly pluginTurns: Set<string>;
  private readonly persistedMessages: Set<string>;
  private readonly persistedTools: Set<string>;
  private readonly pending: Set<string>;
  private lastTurn: string | undefined;
  /** Counts comment events, so a list read across one can tell. */
  private commentEvents = 0;
  /** The count at each comment's latest event. */
  private readonly commentChanged = new Map<number, number>();
  /** The row closing the last turn this stream saw end early. */
  turnEnd: Row | undefined;

  constructor(snapshot: SessionSnapshot, carriedTurnEnd?: Row) {
    if (!snapshot.session?.id) throw new InvalidPayload("The snapshot names no session.");
    const source = snapshot.messages ?? [];
    const active = snapshot.activeTurnId;
    const pendingReplies = source.filter(
      (message) => message.role === "assistant" && message.turnId === active && !message.stopReason,
    );
    const pendingIds = new Set(pendingReplies.map((message) => message.id));
    const rows = transcript(
      source,
      (turn) => turn !== active,
      (turn) => turn !== active,
      carriedTurnEnd ? [carriedTurnEnd] : [],
      active,
    ).filter((row) => !(row.kind === "assistant" && pendingIds.has(row.id.replace(/-text-\d+$/, ""))));
    if (carriedTurnEnd && rows.some((row) => row.id === carriedTurnEnd.id)) this.turnEnd = carriedTurnEnd;

    this.userTurns = new Set(source.filter((m) => m.role === "user").map((m) => m.turnId));
    this.pluginTurns = new Set(
      source.filter((m) => m.role === "context" && m.boundaryKind === "plugin_message").map((m) => m.turnId),
    );
    this.persistedMessages = new Set(source.map((m) => m.id).filter((id) => !pendingIds.has(id)));
    this.persistedTools = new Set(
      source.filter((m) => m.role === "tool" && m.toolCallId).map((m) => m.toolCallId as string),
    );
    if (!snapshot.eventReplayAvailable) {
      for (const message of pendingReplies) {
        (message.content ?? []).forEach((block, index) => {
          const buffer =
            block.kind === "text" ? this.text : block.kind === "thinking" ? this.thinking : undefined;
          if (!buffer || !("text" in block)) return;
          const blocks = buffer.get(message.id) ?? new Map<number, string>();
          blocks.set(index, block.text);
          buffer.set(message.id, blocks);
        });
      }
    }
    this.lastTurn = source.at(-1)?.turnId;
    const interactions = (snapshot.pendingInteractions ?? []).filter(
      (interaction) => interaction.plugin || interaction.turnId === active,
    );
    this.pending = new Set(interactions.map((interaction) => interaction.id));
    this.state = {
      info: snapshot.session,
      rows,
      activeTurnId: active,
      activeShellId: snapshot.activeBashExecutionId || undefined,
      activity: snapshot.activeCompaction ? COMPACTING : active ? WORKING : undefined,
      compacting: Boolean(snapshot.activeCompaction),
      status: "idle",
      pendingInteractions: snapshot.pendingInteractions ?? [],
      followUps: snapshot.followUps,
      contextTokens: snapshot.contextTokens,
      contextWindow: snapshot.contextWindow,
      usage: snapshot.usage,
      retryAt: snapshot.providerRetry?.retryAt,
      retryCount: snapshot.providerRetry?.count,
      hasMoreMessages: snapshot.hasMoreMessages ?? false,
      previousMessageCursor: snapshot.previousMessageCursor,
      workspace: snapshot.workspace,
      scratchpad: snapshot.scratchpad,
      comments: (snapshot.annotations ?? []).map(fromSummary).sort((a, b) => a.id - b.id),
      subagents: snapshot.subagentConversations ?? [],
    };
    this.state.status = this.status();
  }

  /** The current state; a new object after every change. */
  get session(): SessionState {
    return this.state;
  }

  /** Puts older rows from a transcript page before the ones shown. */
  prepend(rows: Row[], hasMore: boolean, cursor: string | undefined): void {
    const shown = new Set(this.state.rows.map((row) => row.id));
    this.state = {
      ...this.state,
      rows: [...rows.filter((row) => !shown.has(row.id)), ...this.state.rows],
      hasMoreMessages: hasMore,
      previousMessageCursor: cursor,
    };
  }

  /** A mark to pass to `setComments` with the list read after it. */
  get commentMark(): number {
    return this.commentEvents;
  }

  /**
   * Takes a fresh list of the session's comments, read after `mark`. A list
   * read across a comment event only refreshes the comments it names.
   */
  setComments(listed: Annotation[], mark: number): void {
    const fresh = listed.map((annotation) => fromAnnotation(annotation)).sort((a, b) => a.id - b.id);
    this.state = {
      ...this.state,
      comments: mergeComments(
        this.state.comments,
        fresh,
        mark === this.commentEvents,
        (id) => (this.commentChanged.get(id) ?? 0) > mark,
      ),
    };
  }

  /** Takes the subagents' conversations from a snapshot read after `subagent.changed`. */
  setSubagents(conversations: SubagentConversation[]): void {
    this.state = { ...this.state, subagents: conversations };
  }

  /**
   * Gives a live user row the attachments and comments its stored message
   * has; the live event carries only the text. Returns whether the row changed.
   */
  fillUserRow(message: TranscriptMessage): boolean {
    const id = `live-user-${message.turnId}`;
    const media = attachments(message.content ?? []);
    const comments = sentComments(message.content ?? []);
    const index = this.state.rows.findIndex((row) => row.id === id);
    const row = this.state.rows[index];
    if (message.role !== "user" || (media.length === 0 && comments.length === 0) || row?.kind !== "user")
      return false;
    const filled: Row = { ...row, attachments: media, comments };
    this.state = {
      ...this.state,
      rows: this.state.rows.map((value, position) => (position === index ? filled : value)),
    };
    return true;
  }

  /** Applies one event; `now` is when a new event arrived, undefined when replayed. */
  apply(event: SessionEvent, now?: number): void {
    if (event.sessionId !== this.state.info.id)
      throw new InvalidPayload("An event belongs to another session.");
    const next: SessionState = { ...this.state };
    this.state = next;
    switch (event.kind) {
      case "turn.started":
        if (next.activeTurnId !== event.turnId) this.retainPluginInteractions(next);
        next.activeTurnId = event.turnId;
        next.compacting = false;
        next.activity = WORKING;
        this.turnEnd = undefined;
        break;
      case "interaction.requested": {
        const interaction = event.interaction;
        if (interaction.plugin || interaction.turnId === next.activeTurnId) {
          this.pending.add(interaction.id);
          next.pendingInteractions = [
            ...next.pendingInteractions.filter((value) => value.id !== interaction.id),
            interaction,
          ];
        }
        break;
      }
      case "interaction.resolved":
        this.pending.delete(event.interactionId);
        next.pendingInteractions = next.pendingInteractions.filter(
          (value) => value.id !== event.interactionId,
        );
        break;
      case "user.message.added": {
        const turn = event.turnId ?? "";
        if (!this.userTurns.has(turn)) {
          this.userTurns.add(turn);
          next.rows = [
            ...next.rows,
            { kind: "user", id: `live-user-${turn}`, text: event.text, attachments: [], comments: [] },
          ];
        }
        this.lastTurn = turn;
        break;
      }
      case "boundary.created": {
        if (event.boundaryKind === "plugin_message") break;
        const row = contextRow({
          id: event.boundaryId,
          boundaryId: event.boundaryId,
          boundaryKind: event.boundaryKind,
          boundarySource: event.boundarySource,
          content: event.content,
          details: event.details,
        });
        if (row) this.upsert(next, row);
        break;
      }
      case "plugin.message.added": {
        const turn = event.turnId ?? "";
        if (!event.pluginId || !event.text || !turn)
          throw new InvalidPayload("A plugin message is incomplete.");
        if (!this.pluginTurns.has(turn)) {
          this.pluginTurns.add(turn);
          next.rows = [
            ...next.rows,
            { kind: "plugin", id: `live-plugin-${turn}`, text: event.text, plugin: event.pluginId },
          ];
        }
        this.lastTurn = turn;
        break;
      }
      case "assistant.started":
      case "assistant.text.delta":
      case "assistant.thinking.delta": {
        const id = event.messageId;
        if (!id) throw new InvalidPayload("An assistant event names no message.");
        if (this.persistedMessages.has(id)) break;
        if (event.kind === "assistant.started") {
          this.text.set(id, new Map(event.text ? [[-1, event.text]] : []));
          this.thinking.set(id, new Map(event.thinking ? [[-1, event.thinking]] : []));
        } else {
          const index = event.contentIndex ?? 0;
          if (index < 0) throw new InvalidPayload("A delta has a negative content index.");
          const buffer = event.kind === "assistant.text.delta" ? this.text : this.thinking;
          const blocks = buffer.get(id) ?? new Map<number, string>();
          blocks.set(index, (blocks.get(index) ?? "") + event.delta);
          buffer.set(id, blocks);
        }
        if (event.kind === "assistant.thinking.delta") {
          next.activity = latestLine(joined(this.thinking.get(id))) ?? THINKING;
        } else {
          next.activity = WORKING;
        }
        break;
      }
      case "assistant.completed": {
        const id = event.messageId;
        if (!id) throw new InvalidPayload("An assistant event names no message.");
        const text = event.text || joined(this.text.get(id));
        if (!this.persistedMessages.has(id) && text) {
          const row: Row = { kind: "assistant", id, text, attachments: [] };
          this.upsert(next, row);
        }
        this.text.delete(id);
        next.activity = WORKING;
        this.lastTurn = event.turnId;
        break;
      }
      case "tool.planned":
      case "tool.started":
      case "tool.output.delta":
      case "tool.completed":
        this.applyTool(next, event, now);
        break;
      case "provider.retry.scheduled":
        next.retryAt = event.providerRetry.retryAt;
        next.retryCount = event.providerRetry.count;
        break;
      case "provider.retry.started":
        next.retryAt = undefined;
        next.retryCount = undefined;
        break;
      case "turn.completed": {
        this.settleTools(next, now);
        next.compacting = false;
        const error = event.errorMessage?.trim();
        if (error && event.turnId) {
          const end: TurnEnd =
            event.status === "aborted"
              ? "stopped"
              : event.status === "interrupted"
                ? "interrupted"
                : "failed";
          const row = turnEndRow(event.turnId, end, error);
          if (!next.rows.some((value) => value.id === row.id)) next.rows = [...next.rows, row];
          this.turnEnd = row;
        }
        next.retryAt = undefined;
        next.retryCount = undefined;
        next.activeTurnId = undefined;
        this.retainPluginInteractions(next);
        next.activity = undefined;
        this.text.clear();
        this.thinking.clear();
        break;
      }
      case "compaction.started":
        next.compacting = true;
        next.activity = COMPACTING;
        break;
      case "compaction.completed":
        next.compacting = false;
        next.activity = next.activeTurnId ? WORKING : undefined;
        break;
      case "usage.changed":
        next.usage = event.usage;
        break;
      case "context.changed":
        next.contextTokens = event.contextTokens;
        next.contextWindow = event.contextWindow;
        break;
      case "session.name.changed":
        next.info = { ...next.info, name: event.sessionName };
        break;
      case "scratchpad.changed":
        if (newerScratchpad(event.scratchpad, next.scratchpad)) next.scratchpad = event.scratchpad;
        break;
      case "session.cwd.changed":
        next.info = { ...next.info, cwd: event.workspace.cwd };
        next.workspace = event.workspace;
        break;
      case "annotation.created":
      case "annotation.updated": {
        this.commentEvents += 1;
        this.commentChanged.set(event.annotation.id, this.commentEvents);
        const old = next.comments.find((comment) => comment.id === event.annotation.id);
        // An edit changes the words, not whether the source moved under them.
        const comment = fromAnnotation(event.annotation, old?.stale ?? event.annotation.stale === true);
        if (old?.stale) comment.staleReason = old.staleReason;
        next.comments = old
          ? next.comments.map((value) => (value.id === comment.id ? comment : value))
          : [...next.comments, comment].sort((a, b) => a.id - b.id);
        break;
      }
      case "annotation.deleted":
        this.commentEvents += 1;
        next.comments = next.comments.filter((comment) => comment.id !== event.annotationId);
        break;
      case "annotation.submitted": {
        this.commentEvents += 1;
        const sent = new Set(event.annotationIds);
        next.comments = next.comments.filter((comment) => !sent.has(comment.id));
        break;
      }
      case "session.configuration.changed": {
        const configuration = event.configuration;
        if (configuration.configurationRevision < next.info.configurationRevision) break;
        next.info = {
          ...next.info,
          model: configuration.model,
          thinkingLevel: configuration.thinkingLevel,
          inputs: configuration.inputs,
          configurationRevision: configuration.configurationRevision,
        };
        break;
      }
      case "follow_ups.changed":
        next.followUps = event.followUps;
        break;
      default:
        // Other kinds change nothing this client shows.
        break;
    }
    next.status = this.status();
  }

  private applyTool(
    next: SessionState,
    event: Extract<
      SessionEvent,
      { kind: "tool.planned" | "tool.started" | "tool.output.delta" | "tool.completed" }
    >,
    now: number | undefined,
  ): void {
    const id = event.toolCallId;
    if (!id || !event.toolName) throw new InvalidPayload("A tool event names no call.");
    if (this.persistedTools.has(id)) return;
    next.activity = WORKING;
    let index = next.rows.findLastIndex(
      (row) => row.kind === "tools" && row.tools.some((tool) => tool.id === id),
    );
    if (index < 0) {
      const last = next.rows.at(-1);
      if (this.lastTurn === event.turnId && last?.kind === "tools") {
        index = next.rows.length - 1;
      } else {
        next.rows = [...next.rows, { kind: "tools", id: `tools-${id}`, tools: [] }];
        index = next.rows.length - 1;
      }
    }
    const row = next.rows[index] as Extract<Row, { kind: "tools" }>;
    const old = row.tools.find((tool) => tool.id === id);
    const completed = event.kind === "tool.completed";
    const content = "content" in event ? (event.content ?? []) : [];
    const output = visibleText(content);
    const previous = old?.output ?? "";
    const startedAt =
      old?.startedAt ?? (event.kind === "tool.planned" || completed || now === undefined ? undefined : now);
    const tool: ToolCall = {
      id,
      name: event.toolName,
      arguments: ("arguments" in event ? event.arguments : undefined) ?? old?.arguments,
      output: completed ? output : previous + output,
      attachments: completed ? attachments(content) : (old?.attachments ?? []),
      failed: ("isError" in event ? event.isError : undefined) ?? old?.failed ?? false,
      status: completed ? "done" : event.kind === "tool.planned" ? "planned" : "running",
      details: (completed ? object(event.details) : undefined) ?? old?.details,
      startedAt,
      finishedAt: completed && startedAt !== undefined ? now : undefined,
    };
    const tools = old ? row.tools.map((value) => (value.id === id ? tool : value)) : [...row.tools, tool];
    next.rows = next.rows.map((value, position) => (position === index ? { ...row, tools } : value));
    this.lastTurn = event.turnId;
  }

  /** Planned calls never ran once their turn ends; running ones whose completion was missed are done. */
  private settleTools(next: SessionState, now: number | undefined): void {
    next.rows = next.rows.map((row) => {
      if (
        row.kind !== "tools" ||
        !row.tools.some((tool) => tool.status === "planned" || tool.status === "running")
      ) {
        return row;
      }
      return {
        ...row,
        tools: row.tools.map((tool) => {
          if (tool.status === "planned") return { ...tool, status: "notRun" as const };
          if (tool.status === "running") {
            return {
              ...tool,
              status: "done" as const,
              finishedAt: tool.startedAt === undefined ? undefined : now,
            };
          }
          return tool;
        }),
      };
    });
  }

  private retainPluginInteractions(next: SessionState): void {
    next.pendingInteractions = next.pendingInteractions.filter((interaction) => interaction.plugin);
    this.pending.clear();
    for (const interaction of next.pendingInteractions) this.pending.add(interaction.id);
  }

  private upsert(next: SessionState, row: Row): void {
    const index = next.rows.findLastIndex((value) => value.id === row.id);
    next.rows = index < 0 ? [...next.rows, row] : next.rows.map((value, i) => (i === index ? row : value));
  }

  private status(): Status {
    if (this.pending.size > 0) return "awaitingResponse";
    return this.state.activeTurnId ? "running" : "idle";
  }
}
