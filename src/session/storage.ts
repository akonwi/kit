/**
 * Session file storage — read/write ~/.pi-kit/sessions/<id>.json
 */

import { mkdir, readdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { SESSION_VERSION, type Session, type SessionSummary } from "./types";

export const SESSIONS_DIR = join(homedir(), ".pi-kit", "sessions");

function sessionsDir(): string {
  return SESSIONS_DIR;
}

function sessionPath(id: string): string {
  return join(sessionsDir(), `${id}.json`);
}

async function ensureSessionsDir(): Promise<void> {
  await mkdir(sessionsDir(), { recursive: true });
}

function now(): string {
  return new Date().toISOString();
}

function firstUserMessage(messages: AgentMessage[]): string | undefined {
  for (const msg of messages) {
    if (msg.role !== "user") continue;
    const content = (msg as { content?: unknown }).content;
    if (typeof content === "string") return content.slice(0, 120);
    if (Array.isArray(content)) {
      for (const block of content) {
        if (block && typeof block === "object" && (block as any).type === "text") {
          return ((block as any).text as string).slice(0, 120);
        }
      }
    }
  }
  return undefined;
}

export function toSummary(session: Session): SessionSummary {
  return {
    id: session.id,
    cwd: session.cwd,
    name: session.name,
    model: session.model,
    createdAt: session.createdAt,
    updatedAt: session.updatedAt,
    messageCount: session.messages.length,
    firstMessage: firstUserMessage(session.messages),
  };
}

// --- CRUD ---

export async function createSession(cwd: string, model?: string): Promise<Session> {
  await ensureSessionsDir();
  const session: Session = {
    id: randomUUID(),
    version: SESSION_VERSION,
    cwd,
    model,
    createdAt: now(),
    updatedAt: now(),
    messages: [],
  };
  await writeSession(session);
  return session;
}

export async function readSession(id: string): Promise<Session | null> {
  const path = sessionPath(id);
  try {
    const raw = await readFile(path, "utf8");
    return JSON.parse(raw) as Session;
  } catch {
    return null;
  }
}

export async function writeSession(session: Session): Promise<void> {
  await ensureSessionsDir();
  const path = sessionPath(session.id);
  const tmp = `${path}.tmp`;
  await writeFile(tmp, JSON.stringify(session, null, 2), "utf8");
  await rename(tmp, path);
}

export async function updateSession(
  session: Session,
  changes: Partial<Pick<Session, "name" | "model" | "messages">>,
): Promise<Session> {
  const updated: Session = {
    ...session,
    ...changes,
    updatedAt: now(),
  };
  await writeSession(updated);
  return updated;
}

export async function appendMessages(
  session: Session,
  messages: AgentMessage[],
  model?: string,
): Promise<Session> {
  return updateSession(session, {
    messages: [...session.messages, ...messages],
    ...(model ? { model } : {}),
  });
}

export async function deleteSession(id: string): Promise<void> {
  const path = sessionPath(id);
  await rm(path, { force: true });
}

// --- Listing ---

export async function listAllSessions(): Promise<SessionSummary[]> {
  const dir = sessionsDir();
  if (!existsSync(dir)) return [];

  const files = (await readdir(dir)).filter(f => f.endsWith(".json"));
  const summaries: SessionSummary[] = [];

  await Promise.all(
    files.map(async (file) => {
      const id = file.replace(/\.json$/, "");
      const session = await readSession(id);
      if (session) summaries.push(toSummary(session));
    }),
  );

  return summaries.sort(
    (a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
  );
}

export async function listSessionsForCwd(cwd: string): Promise<SessionSummary[]> {
  const all = await listAllSessions();
  return all.filter(s => s.cwd === cwd);
}

export async function findSessionById(idPrefix: string): Promise<Session | null> {
  const dir = sessionsDir();
  if (!existsSync(dir)) return null;

  const files = (await readdir(dir)).filter(f => f.endsWith(".json"));
  const needle = idPrefix.toLowerCase();

  for (const file of files) {
    const id = file.replace(/\.json$/, "");
    if (id.toLowerCase().startsWith(needle)) {
      return readSession(id);
    }
  }

  return null;
}

/** Open the most recent session for cwd, or create a new one. */
export async function openRecentSession(cwd: string, model?: string): Promise<Session> {
  const sessions = await listSessionsForCwd(cwd);
  if (sessions.length > 0) {
    const session = await readSession(sessions[0].id);
    if (session) return session;
  }
  return createSession(cwd, model);
}
