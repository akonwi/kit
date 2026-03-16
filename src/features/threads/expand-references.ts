/**
 * Expand [[thread:id]] tokens in user message text.
 *
 * Resolves each token to a session, reads the session's recent messages,
 * and replaces the token with a formatted reference block.
 */

import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { SessionManager, type SessionInfo } from "@mariozechner/pi-coding-agent";

const MAX_REFERENCES_PER_PROMPT = 3;
const MAX_BLOCK_CHARS = 3500;
const MAX_LINE_CHARS = 280;
const MAX_LINES = 12;

// ── Helpers ─────────────────────────────────────────────────────────

function clip(text: string, max: number): string {
  return text.length <= max ? text : `${text.slice(0, max - 1)}…`;
}

function messageText(msg: AgentMessage): string {
  const content: unknown = (msg as { content?: unknown }).content;
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";

  return content
    .map((block) => {
      if (!block || typeof block !== "object") return "";
      const b = block as Record<string, unknown>;
      if (b.type === "text" && typeof b.text === "string") return b.text;
      return "";
    })
    .filter(Boolean)
    .join("\n")
    .trim();
}

function roleLabel(role: string): string {
  if (role === "user") return "User";
  if (role === "assistant") return "Assistant";
  if (role === "toolResult") return "Tool";
  if (role === "bashExecution") return "Bash";
  return "Message";
}

function threadTitle(s: SessionInfo): string {
  const head = (s.name?.trim() || s.firstMessage?.trim() || "Untitled thread").replace(/\s+/g, " ");
  return clip(head, 80);
}

// ── Resolution ──────────────────────────────────────────────────────

function resolveToken(
  token: string,
  sessions: SessionInfo[],
): { session?: SessionInfo; error?: string } {
  const key = token.trim().toLowerCase();
  if (!key) return { error: "empty reference" };

  const byIdPrefix = sessions.filter((s) => s.id.toLowerCase().startsWith(key));
  if (byIdPrefix.length === 1) return { session: byIdPrefix[0] };
  if (byIdPrefix.length > 1) return { error: `ambiguous id prefix '${token}'` };

  const byNameContains = sessions.filter((s) =>
    `${s.name || ""} ${s.firstMessage || ""}`.toLowerCase().includes(key),
  );
  if (byNameContains.length === 1) return { session: byNameContains[0] };
  if (byNameContains.length > 1) return { error: `ambiguous name match '${token}'` };

  return { error: `no thread found for '${token}'` };
}

// ── Reference block ─────────────────────────────────────────────────

function buildReferenceBlock(session: SessionInfo): string {
  try {
    const sm = SessionManager.open(session.path);
    const context = sm.buildSessionContext();

    const messages = context.messages
      .filter((m) => m.role === "user" || m.role === "assistant" || m.role === "toolResult")
      .map((m) => {
        const text = messageText(m).replace(/\s+/g, " ").trim();
        return {
          role: roleLabel(m.role),
          text: clip(text, MAX_LINE_CHARS),
        };
      })
      .filter((m) => m.text.length > 0);

    const tail = messages.slice(-MAX_LINES);

    const header = [
      "[Thread Reference]",
      `id: ${session.id}`,
      `title: ${threadTitle(session)}`,
      `cwd: ${session.cwd || "(unknown)"}`,
      `updated: ${session.modified.toISOString()}`,
      "---",
    ];

    const body = tail.map((m) => `${m.role}: ${m.text}`);
    const block = [...header, ...body].join("\n");
    return clip(block, MAX_BLOCK_CHARS);
  } catch (error) {
    const msg = error instanceof Error ? error.message : String(error);
    return `[Thread Reference]\nid: ${session.id}\nerror: failed to read thread (${msg})`;
  }
}

// ── Public API ──────────────────────────────────────────────────────

export type ExpandResult = {
  text: string;
  expanded: number;
  errors: string[];
};

/**
 * Expand all [[thread:token]] placeholders in the given text.
 * Returns the transformed text, count of expanded references, and any errors.
 */
export async function expandThreadReferences(
  text: string,
  currentSessionPath?: string,
): Promise<ExpandResult> {
  const matches = [...text.matchAll(/\[\[thread:([^\]]+)\]\]/gi)];
  if (matches.length === 0) {
    return { text, expanded: 0, errors: [] };
  }

  const uniqueTokens = Array.from(
    new Set(matches.map((m) => (m[1] || "").trim())),
  ).slice(0, MAX_REFERENCES_PER_PROMPT);

  const allSessions = await SessionManager.listAll();
  const sessions = allSessions.filter(
    (s) => !currentSessionPath || s.path !== currentSessionPath,
  );

  let transformed = text;
  let expanded = 0;
  const errors: string[] = [];

  for (const token of uniqueTokens) {
    const placeholder = `[[thread:${token}]]`;
    const resolved = resolveToken(token, sessions);

    if (!resolved.session) {
      errors.push(`${placeholder}: ${resolved.error || "unknown error"}`);
      continue;
    }

    const block = buildReferenceBlock(resolved.session);
    const replacement = `\n\n${block}\n\n`;
    transformed = transformed.split(placeholder).join(replacement);
    expanded++;
  }

  if (matches.length > MAX_REFERENCES_PER_PROMPT) {
    errors.push(`Only first ${MAX_REFERENCES_PER_PROMPT} thread references were expanded.`);
  }

  return { text: transformed, expanded, errors };
}
