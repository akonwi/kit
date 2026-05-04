/**
 * Kit session types.
 *
 * Runtime sessions remain turn-based, but persisted session storage is an
 * append-only JSONL entry log under ~/.kit/sessions/<id>.jsonl.
 */

import type { AgentMessage, ThinkingLevel } from "@mariozechner/pi-agent-core";

export const SESSION_VERSION = 2;

/** AgentMessage tagged with the turn it belongs to. */
export type SyntheticSummaryKind = "compaction-summary" | "handoff-summary";

export type KitAgentMessage = AgentMessage & {
	turnId: string;
	synthetic?: {
		kind: SyntheticSummaryKind;
		sourceSessionName?: string;
	};
};

export type PersistedKitAgentMessage = AgentMessage & {
	synthetic?: {
		kind: SyntheticSummaryKind;
		sourceSessionName?: string;
	};
};

/** A single agent turn: one user prompt + all resulting messages. */
export interface Turn {
	id: string;
	messages: KitAgentMessage[];
}

export interface Session {
	/** UUID */
	id: string;
	version: typeof SESSION_VERSION;
	/** Absolute path to the working directory when session was created */
	cwd: string;
	/** Parent session ID when this session was forked/handed off */
	parentSessionId?: string;
	/** Parent turn boundary captured at handoff time */
	forkedFromTurnId?: string;
	/** User-assigned display name */
	name?: string;
	/** Model ID at time of last message, e.g. "claude-sonnet-4" */
	model?: string;
	/** Current session thinking level */
	thinkingLevel?: ThinkingLevel;
	createdAt: string; // ISO 8601
	updatedAt: string; // ISO 8601
	turns: Turn[];
}

/** Lightweight summary for listings — no messages */
export interface SessionSummary {
	id: string;
	cwd: string;
	parentSessionId?: string;
	forkedFromTurnId?: string;
	name?: string;
	model?: string;
	thinkingLevel?: ThinkingLevel;
	createdAt: string;
	updatedAt: string;
	messageCount: number;
	/** First user message text, truncated */
	firstMessage?: string;
}

export interface SessionHeader {
	type: "session";
	version: typeof SESSION_VERSION;
	id: string;
	createdAt: string;
	cwd: string;
	parentSessionId?: string;
	forkedFromTurnId?: string;
	name?: string;
	model?: string;
	thinkingLevel?: ThinkingLevel;
}

export interface SessionEntryBase {
	type: string;
	id: string;
	parentId: string | null;
	timestamp: string;
}

export interface SessionMessageEntry extends SessionEntryBase {
	type: "message";
	turnId: string;
	message: PersistedKitAgentMessage;
}

export interface SessionInfoEntry extends SessionEntryBase {
	type: "session_info";
	name?: string;
}

export interface SessionModelChangeEntry extends SessionEntryBase {
	type: "model_change";
	modelId?: string;
}

export interface SessionThinkingLevelChangeEntry extends SessionEntryBase {
	type: "thinking_level_change";
	thinkingLevel?: ThinkingLevel;
}

export interface SessionCompactionEntry extends SessionEntryBase {
	type: "compaction";
	firstKeptEntryId?: string;
	compactedTurnCount: number;
	keptTurnCount: number;
	tokensBefore: number;
	message: PersistedKitAgentMessage;
}

export interface SessionHandoffSummaryEntry extends SessionEntryBase {
	type: "handoff_summary";
	message: PersistedKitAgentMessage;
}

export type SessionEntry =
	| SessionMessageEntry
	| SessionInfoEntry
	| SessionModelChangeEntry
	| SessionThinkingLevelChangeEntry
	| SessionCompactionEntry
	| SessionHandoffSummaryEntry;

export type SessionFileEntry = SessionHeader | SessionEntry;
