// Shell commands (`!` and `!!`) as Kit's other clients show them: a command
// the agent sees is recorded in the transcript as a `bash` boundary when it
// finishes; one it doesn't see (`!!`) is never recorded, so it shows only on
// the device that ran it, after the row that was last when it was sent.
import { type Accessor, createSignal, onCleanup } from "solid-js";
import { type Api, ApiError, NetworkError } from "../api/client";
import type { TranscriptContent } from "../api/types";
import type { Row } from "./projection";

/** One shell command and, once it settles, how it went. */
export interface ShellRun {
  id: string;
  command: string;
  /** `running`, `completed`, `failed`, or `aborted`. */
  status: string;
  output: string;
  exitCode?: number;
  /** Kept from the agent's context (`!!`). */
  excluded: boolean;
  truncated: boolean;
  timedOut: boolean;
  error?: string;
}

/** What a draft asks to run: `!command`, or `!!command` to keep it from the agent. */
export function shellDraft(text: string): { command: string; excluded: boolean } | undefined {
  if (!text.startsWith("!")) return undefined;
  const excluded = text.startsWith("!!");
  return { command: text.slice(excluded ? 2 : 1).trim(), excluded };
}

/** The draft with its shell prefix switched between `!` and `!!`. */
export function toggleShellContext(text: string): string {
  const draft = shellDraft(text);
  if (!draft) return text;
  return (draft.excluded ? "!" : "!!") + text.slice(draft.excluded ? 2 : 1);
}

/**
 * The outcome a row names beside its command. A running command shows a
 * spinner and one that succeeded needs no label, as on the Mac.
 */
export function shellStatus(run: ShellRun): string | undefined {
  if (run.status === "running") return undefined;
  if (run.timedOut) return "Timed out";
  if (run.status === "completed")
    return run.exitCode && run.exitCode !== 0 ? `Exit ${run.exitCode}` : undefined;
  return run.status.charAt(0).toUpperCase() + run.status.slice(1);
}

/** A command the server reports, or undefined if it breaks the contract. */
export function shellFromWire(wire: {
  id: string;
  command: string;
  status: string;
  output?: string;
  exitCode?: number;
  excludeFromContext?: boolean;
  truncated?: boolean;
  timedOut?: boolean;
  errorMessage?: string;
}): ShellRun | undefined {
  if (!wire.id || !wire.command || !wire.status) return undefined;
  return {
    id: wire.id,
    command: wire.command,
    status: wire.status,
    output: wire.output ?? "",
    exitCode: wire.exitCode,
    excluded: wire.excludeFromContext === true,
    truncated: wire.truncated === true,
    timedOut: wire.timedOut === true,
    error: wire.errorMessage || undefined,
  };
}

/**
 * A recorded command from its `bash` boundary: the details name the command
 * and outcome, and the text the agent saw carries the output between the
 * server's framing (`[bash from SOURCE]`), the command line, and the notices
 * the server appends.
 */
export function shellFromBoundary(
  id: string,
  details: unknown,
  content: readonly TranscriptContent[],
  source?: string,
): ShellRun | undefined {
  if (typeof details !== "object" || details === null) return undefined;
  const value = details as Record<string, unknown>;
  if (value.version !== 1 || typeof value.command !== "string" || !value.command) return undefined;
  if (typeof value.status !== "string") return undefined;
  const command = value.command;
  const exitCode = typeof value.exitCode === "number" ? value.exitCode : undefined;
  const timedOut = value.timedOut === true;
  const truncated = value.truncated === true;
  const error = typeof value.errorMessage === "string" && value.errorMessage ? value.errorMessage : undefined;
  let output = withoutFraming(content, source)
    .filter((block) => block.kind === "text")
    .map((block) => (block.kind === "text" ? block.text : ""))
    .join("\n\n");
  const prefix = `[bash command: ${command}]${exitCode ? ` (exit code: ${exitCode})` : ""}`;
  // The server joins the command line, the output, and each notice with one newline.
  if (output.startsWith(prefix)) output = output.slice(prefix.length).replace(/^\n/, "");
  const notices = [
    timedOut ? "[timed out]" : undefined,
    truncated ? "[output truncated]" : undefined,
    error ? `[${value.status}: ${error}]` : undefined,
  ].filter((notice): notice is string => notice !== undefined);
  for (const notice of notices.reverse()) {
    if (output.endsWith(notice)) output = output.slice(0, -notice.length).replace(/\n$/, "");
  }
  output = output.replace(/\n+$/, "");
  return { id, command, status: value.status, output, exitCode, excluded: false, truncated, timedOut, error };
}

/** The boundary's content without the framing line the server puts first. */
function withoutFraming(
  content: readonly TranscriptContent[],
  source?: string,
): readonly TranscriptContent[] {
  const framing = `[bash${source ? ` from ${source}` : ""}]`;
  const [first, ...rest] = content;
  if (first?.kind !== "text") return content;
  if (first.text === framing) return rest;
  if (first.text.startsWith(`${framing}\n`))
    return [{ ...first, text: first.text.slice(framing.length + 1) }, ...rest];
  return content;
}

/**
 * Places this device's commands among the transcript's rows. A command the
 * transcript has recorded is shown from its record. Any other goes after the
 * row that was last when it was sent (at the start when there was none); if
 * that row is no longer shown, a running command follows the transcript and
 * a settled one is left out, since its place isn't shown.
 */
export function placeShellRuns(
  rows: readonly Row[],
  runs: readonly ShellRun[],
  anchors: ReadonlyMap<string, string>,
) {
  if (runs.length === 0) return rows as Row[];
  const shown = new Set(rows.map((row) => row.id));
  const after = new Map<string, Row[]>();
  const first: Row[] = [];
  const last: Row[] = [];
  for (const run of runs) {
    if (shown.has(run.id)) continue;
    const row: Row = { kind: "shell", id: run.id, run };
    const anchor = anchors.get(run.id);
    if (anchor === "") first.push(row);
    else if (anchor !== undefined && shown.has(anchor))
      after.set(anchor, [...(after.get(anchor) ?? []), row]);
    else if (run.status === "running") last.push(row);
  }
  const placed: Row[] = [...first];
  for (const row of rows) {
    placed.push(row);
    const following = after.get(row.id);
    if (following) placed.push(...following);
  }
  return [...placed, ...last];
}

/**
 * A fresh command id, chosen by the client so a lost answer can be looked up.
 * getRandomValues works on a plain-http page, where randomUUID doesn't.
 */
export function newShellId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return `bash_${Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

const POLL_MS = 1000;
const KEEP = 64;

export interface Shell {
  /** This device's commands, oldest first. */
  runs: Accessor<ShellRun[]>;
  /** Where each command goes: the row last shown when it was sent. */
  anchors: Accessor<ReadonlyMap<string, string>>;
  /** The command still running, if any. */
  active: Accessor<ShellRun | undefined>;
  starting: Accessor<boolean>;
  stopping: Accessor<boolean>;
  /** Runs a draft's command after the row `after`; resolves true once the server took it. */
  start(text: string, after: string): Promise<boolean>;
  /** Follows a command started elsewhere, such as the snapshot's running one. */
  follow(id: string): void;
  /** Takes a command's outcome from the transcript, which can record it before a poll sees it. */
  settle(run: ShellRun): void;
  stop(): Promise<void>;
}

/** This device's shell commands in a session, kept current while they run. */
export function createShell(api: Api, session: Accessor<string>, fail: (error: unknown) => void): Shell {
  const [runs, setRuns] = createSignal<ShellRun[]>([]);
  const [anchors, setAnchors] = createSignal<ReadonlyMap<string, string>>(new Map());
  const [starting, setStarting] = createSignal(false);
  const [stopping, setStopping] = createSignal(false);
  // An answer lost in flight leaves the command's id to look up or reuse, so
  // a retry of the same draft never starts a second command.
  let admission: { id: string; text: string } | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let disposed = false;

  const record = (run: ShellRun) => {
    setRuns((current) => {
      const index = current.findIndex((value) => value.id === run.id);
      // A settled command never goes back to running.
      if (index >= 0 && current[index]?.status !== "running" && run.status === "running") return current;
      const next = index >= 0 ? current.map((value, at) => (at === index ? run : value)) : [...current, run];
      return next.slice(-KEEP);
    });
    schedule();
  };

  const active = () => runs().findLast((run) => run.status === "running");

  const poll = async () => {
    timer = undefined;
    for (const run of runs().filter((value) => value.status === "running")) {
      try {
        const next = shellFromWire(await api.getBash(session(), run.id));
        if (next) record(next);
      } catch (failure) {
        if (failure instanceof NetworkError) continue;
        fail(failure);
      }
    }
    schedule();
  };

  function schedule() {
    if (disposed || timer || !active()) return;
    timer = setTimeout(() => void poll(), POLL_MS);
  }

  onCleanup(() => {
    disposed = true;
    clearTimeout(timer);
  });

  return {
    runs,
    anchors,
    active,
    starting,
    stopping,
    async start(text, after) {
      const draft = shellDraft(text);
      if (!draft?.command || starting() || active()) return false;
      if (admission && admission.text !== text) admission = undefined;
      const id = admission?.id ?? newShellId();
      admission = { id, text };
      setAnchors((current) => (current.has(id) ? current : new Map(current).set(id, after)));
      setStarting(true);
      try {
        let wire: Parameters<typeof shellFromWire>[0];
        try {
          wire = await api.startBash(session(), {
            executionId: id,
            command: draft.command,
            excludeFromContext: draft.excluded,
          });
        } catch (failure) {
          // Whether a lost answer started the command is a lookup away.
          if (!(failure instanceof NetworkError)) {
            admission = undefined;
            throw failure;
          }
          wire = await api.getBash(session(), id);
        }
        admission = undefined;
        const run = shellFromWire(wire);
        if (run) record(run);
        return true;
      } catch (failure) {
        if (failure instanceof ApiError && failure.status === 404) admission = undefined;
        throw failure;
      } finally {
        setStarting(false);
      }
    },
    follow(id) {
      if (runs().some((run) => run.id === id)) return;
      void api
        .getBash(session(), id)
        .then((wire) => {
          const run = shellFromWire(wire);
          if (run) record(run);
        })
        .catch((failure) => {
          if (!(failure instanceof NetworkError)) fail(failure);
        });
    },
    settle(run) {
      const known = runs().find((value) => value.id === run.id);
      if (known?.status === "running" && run.status !== "running")
        record({ ...run, excluded: known.excluded });
    },
    async stop() {
      const run = active();
      if (!run || stopping()) return;
      setStopping(true);
      try {
        await api.abortBash(session(), run.id);
        void poll();
      } finally {
        setStopping(false);
      }
    },
  };
}
