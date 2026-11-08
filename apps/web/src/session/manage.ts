// What creating, renaming, forking, compacting, and deleting a session decide
// before and after asking the server, as the Mac and iOS apps decide it.
import { ApiError, NetworkError } from "../api/client";
import type { CompactionFailureReason, ServerSettings, SessionInfo } from "../api/types";
import { displayPath } from "../ui/format";

/**
 * A fresh id with a prefix, chosen by the client so a lost answer can be
 * looked up: 128 random bits in hex. getRandomValues rather than randomUUID,
 * which a page over plain http on a tailnet address, not a secure context,
 * doesn't have.
 */
export function newId(prefix: "session" | "compact"): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return `${prefix}_${Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

/**
 * The absolute folder a typed path names on a server whose user's home is
 * `home`: `~` is that home, and a relative path starts from it. Without a
 * known home only an absolute path names a folder.
 */
export function folderPath(typed: string, home: string | undefined): string | undefined {
  const path = typed.trim();
  if (!path) return undefined;
  if (path.startsWith("/")) return path;
  if (!home) return undefined;
  const base = home.endsWith("/") ? home.slice(0, -1) : home;
  if (path === "~") return base || "/";
  const relative = path.startsWith("~/") ? path.slice(2) : path;
  return `${base}/${relative}`;
}

/** The folders sessions on the server work in, newest first, as the user types them. */
export function recentFolders(sessions: readonly SessionInfo[], home: string | undefined): string[] {
  const seen = new Set<string>();
  const folders: string[] = [];
  for (const session of [...sessions].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))) {
    if (!session.cwd || seen.has(session.cwd)) continue;
    seen.add(session.cwd);
    folders.push(displayPath(session.cwd, home));
  }
  return folders;
}

/**
 * The recent folders the folder field offers: all of them while the field
 * still shows a folder from the list, else those containing what was typed.
 */
export function folderOptions(folders: readonly string[], typed: string): string[] {
  const query = typed.trim().toLocaleLowerCase();
  if (!query || folders.includes(typed.trim())) return [...folders];
  return folders.filter((folder) => folder.toLocaleLowerCase().includes(query));
}

/** The model a new session starts on: the server's default, else its own choice, else the first. */
export function preferredModel(settings: ServerSettings | undefined, available: readonly string[]): string {
  const choices = [settings?.defaultModel ?? "", settings?.newSessionModel ?? ""];
  return choices.find((model) => model && available.includes(model)) ?? available[0] ?? "";
}

/** The thinking level a new session starts on: medium when offered, else the first, else none. */
export function defaultThinking(levels: readonly string[]): string {
  return levels.includes("medium") ? "medium" : (levels[0] ?? "");
}

/** Whether a failed create certainly made no session: never sent, or refused. */
export function definitelyRejected(error: unknown): boolean {
  if (error instanceof NetworkError) return false;
  return error instanceof ApiError && [400, 401, 403, 404, 409, 422].includes(error.status);
}

/** Why Compact is unavailable now, or undefined when it can run. */
export function compactUnavailable(
  session: { activeTurnId?: string; compacting: boolean },
  pending: boolean,
) {
  if (session.activeTurnId) return "Context can compact when the turn ends";
  if (pending || session.compacting) return "Compaction in progress";
  return undefined;
}

/** Why Fork is unavailable, or undefined. A fork copies the last settled history, so a turn doesn't block it. */
export function forkUnavailable(info: SessionInfo): string | undefined {
  return info.temporary ? "Temporary sessions can’t be forked" : undefined;
}

const PERMANENT_FAILURES: readonly CompactionFailureReason[] = [
  "not_adaptable",
  "policy_stop",
  "summary_truncated",
];

/** How a compaction ended, as the session tells the user. */
export type CompactionOutcome =
  | { tone: "info"; title: string }
  | { tone: "error"; title: string; detail: string; retry: boolean };

export function compactionOutcome(
  result: { compacted: boolean } | undefined,
  error?: unknown,
): CompactionOutcome {
  if (result)
    return { tone: "info", title: result.compacted ? "Session compacted" : "Not enough turns to compact" };
  if (error instanceof ApiError && error.status === 409)
    return {
      tone: "error",
      title: "Compaction failed",
      detail: "The session is busy. Try again when the turn ends.",
      retry: true,
    };
  const reason = error instanceof ApiError ? error.details?.reason : undefined;
  const permanent = PERMANENT_FAILURES.includes(reason as CompactionFailureReason);
  const detail = error instanceof Error ? error.message : "The server didn’t compact the session.";
  return { tone: "error", title: "Compaction failed", detail, retry: !permanent };
}

/** What deleting a session warns: a saved one loses its history; a temporary one leaves the server's memory. */
export function deleteWarning(info: SessionInfo): string {
  const title = info.name || "Untitled session";
  return info.temporary
    ? `“${title}” will be removed from the server’s memory and any active work will be cancelled. This cannot be undone.`
    : `“${title}” and its saved history will be permanently deleted. This cannot be undone.`;
}

/** What a failed delete says. */
export function deleteFailure(error: unknown): string {
  if (error instanceof ApiError && error.status === 409)
    return "This session is in use and cannot be deleted. Stop its active work before trying again.";
  return error instanceof Error ? error.message : "The session wasn’t deleted.";
}

/**
 * What a failed create says. The server answers any invalid create with a bare
 * 400; the model, thinking level, and id come from its catalog and this
 * client, so the folder is what it refused.
 */
export function createFailure(error: unknown, typed: string, host: string): string {
  if (error instanceof ApiError && error.status === 400) return `No folder at ${typed.trim()} on ${host}.`;
  return error instanceof Error ? error.message : "The session wasn’t created.";
}
