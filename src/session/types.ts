/**
 * pi-kit session types.
 *
 * Sessions are stored as JSON files at ~/.pi-kit/sessions/<id>.json.
 * No Pi format compatibility.
 */

import type { AgentMessage } from "@mariozechner/pi-agent-core";

export const SESSION_VERSION = 1;

export interface Session {
  /** UUID */
  id: string;
  version: typeof SESSION_VERSION;
  /** Absolute path to the working directory when session was created */
  cwd: string;
  /** User-assigned display name */
  name?: string;
  /** Model ID at time of last message, e.g. "claude-sonnet-4" */
  model?: string;
  createdAt: string; // ISO 8601
  updatedAt: string; // ISO 8601
  messages: AgentMessage[];
}

/** Lightweight summary for listings — no messages */
export interface SessionSummary {
  id: string;
  cwd: string;
  name?: string;
  model?: string;
  createdAt: string;
  updatedAt: string;
  messageCount: number;
  /** First user message text, truncated */
  firstMessage?: string;
}
