import { readdirSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import {
  SessionManager,
  type SessionEntry,
  type SessionHeader,
  type SessionInfo,
} from "@mariozechner/pi-coding-agent";

const DEFAULT_SESSIONS_DIR = join(homedir(), ".pi", "agent", "sessions");

export type LoadedSession = {
  manager: SessionManager;
  header: SessionHeader | null;
  branch: SessionEntry[];
  cwd: string;
  sessionFile: string | undefined;
  sessionId: string;
  sessionName: string | undefined;
};

export function snapshotLoadedSession(manager: SessionManager): LoadedSession {
  return {
    manager,
    header: manager.getHeader(),
    branch: manager.getBranch(),
    cwd: manager.getCwd(),
    sessionFile: manager.getSessionFile(),
    sessionId: manager.getSessionId(),
    sessionName: manager.getSessionName(),
  };
}

/**
 * Open the most recent session for the given cwd, or create a new one.
 * Uses Pi's SessionManager.continueRecent() which either resumes the latest
 * session file or creates a new one if none exists.
 */
export function openRecentSession(cwd: string, sessionDir?: string): LoadedSession {
  const manager = SessionManager.continueRecent(cwd, sessionDir);
  return snapshotLoadedSession(manager);
}

/**
 * Open a specific session file.
 */
export function openSessionFile(filePath: string, sessionDir?: string): LoadedSession {
  const manager = SessionManager.open(filePath, sessionDir);
  return snapshotLoadedSession(manager);
}

/**
 * List all sessions for a given cwd.
 */
export async function listSessionsForCwd(
  cwd: string,
  sessionDir?: string,
): Promise<SessionInfo[]> {
  return SessionManager.list(cwd, sessionDir);
}

/**
 * List all sessions across all projects.
 */
export async function listAllSessions(): Promise<SessionInfo[]> {
  return SessionManager.listAll();
}

/**
 * Find a session file by its UUID (or UUID prefix) across all session directories.
 * Returns the full path to the .jsonl file, or null if not found.
 */
export function findSessionFileById(sessionId: string): string | null {
  const sessionsRoot = DEFAULT_SESSIONS_DIR;
  let cwdDirs: string[];
  try {
    cwdDirs = readdirSync(sessionsRoot);
  } catch {
    return null;
  }

  const needle = sessionId.toLowerCase();

  for (const cwdDir of cwdDirs) {
    const dirPath = join(sessionsRoot, cwdDir);
    let files: string[];
    try {
      files = readdirSync(dirPath);
    } catch {
      continue;
    }

    for (const file of files) {
      if (!file.endsWith(".jsonl")) continue;
      // Filename format: <timestamp>_<uuid>.jsonl
      const uuidMatch = file.match(/_([0-9a-f-]+)\.jsonl$/);
      if (uuidMatch && uuidMatch[1].toLowerCase().startsWith(needle)) {
        return join(dirPath, file);
      }
    }
  }

  return null;
}
