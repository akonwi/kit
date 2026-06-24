/**
 * Kit session types.
 *
 * Runtime sessions remain turn-based, but persisted session storage is an
 * append-only JSONL entry log under ~/.kit/sessions/<id>.jsonl.
 */

import type { AgentMessage, ThinkingLevel } from "../runtime/agent";

export const SESSION_VERSION = 2;

/** AgentMessage tagged with the turn it belongs to. */
export type SyntheticSummaryKind =
	| "compaction-summary"
	| "handoff-summary"
	| "subagent-delegation";

export type SyntheticMessageMetadata = {
	kind: SyntheticSummaryKind;
	sourceSessionName?: string;
	subagentName?: string;
	subagentDescription?: string;
	subagentPrompt?: string;
	subagentSource?: SubagentEventSource;
};

export type KitAgentMessage = AgentMessage & {
	turnId: string;
	synthetic?: SyntheticMessageMetadata;
};

export type PersistedKitAgentMessage = AgentMessage & {
	synthetic?: SyntheticMessageMetadata;
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
	/** Absolute path to the session's current working directory */
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

export interface SessionCwdChangeEntry extends SessionEntryBase {
	type: "cwd_change";
	cwd: string;
	previousCwd?: string;
	source?: "user" | "agent";
}

export interface SessionCompactionEntry extends SessionEntryBase {
	type: "compaction";
	firstKeptEntryId?: string;
	compactedTurnCount: number;
	keptTurnCount: number;
	tokensBefore: number;
	message: PersistedKitAgentMessage;
}

export type SubagentEventSource = "manual" | "agent";

export interface SubagentEntryBase extends SessionEntryBase {
	agentName: string;
	subagentConversationId: string;
}

export interface SessionHandoffSummaryEntry extends SessionEntryBase {
	type: "handoff_summary";
	message: PersistedKitAgentMessage;
}

export interface SubagentStartedEntry extends SubagentEntryBase {
	type: "subagent_started";
	source: SubagentEventSource;
	model?: string;
	description?: string;
}

export interface SubagentDismissedEntry extends SubagentEntryBase {
	type: "subagent_dismissed";
}

export interface SubagentPromptEntry extends SubagentEntryBase {
	type: "subagent_prompt";
	source: SubagentEventSource;
	prompt: string;
}

export interface SubagentMessageStartedEntry extends SubagentEntryBase {
	type: "subagent_message_started";
	messageId: string;
}

export interface SubagentMessageDeltaEntry extends SubagentEntryBase {
	type: "subagent_message_delta";
	messageId: string;
	delta: string;
}

export interface SubagentMessageCompletedEntry extends SubagentEntryBase {
	type: "subagent_message_completed";
	messageId: string;
	message: PersistedKitAgentMessage;
}

export interface SubagentThinkingStartedEntry extends SubagentEntryBase {
	type: "subagent_thinking_started";
	messageId: string;
}

export interface SubagentThinkingDeltaEntry extends SubagentEntryBase {
	type: "subagent_thinking_delta";
	messageId: string;
	delta: string;
}

export interface SubagentThinkingCompletedEntry extends SubagentEntryBase {
	type: "subagent_thinking_completed";
	messageId: string;
}

export interface SubagentToolStartedEntry extends SubagentEntryBase {
	type: "subagent_tool_started";
	toolCallId: string;
	toolName: string;
	args: unknown;
}

export interface SubagentToolUpdatedEntry extends SubagentEntryBase {
	type: "subagent_tool_updated";
	toolCallId: string;
	toolName: string;
	partialResult: unknown;
}

export interface SubagentToolCompletedEntry extends SubagentEntryBase {
	type: "subagent_tool_completed";
	toolCallId: string;
	toolName: string;
	result: unknown;
	isError: boolean;
}

export interface SubagentFailedEntry extends SubagentEntryBase {
	type: "subagent_failed";
	error: string;
}

export interface SubagentAbortedEntry extends SubagentEntryBase {
	type: "subagent_aborted";
	reason?: string;
}

export interface SubagentCompactionEntry extends SubagentEntryBase {
	type: "subagent_compaction";
	firstKeptTurnId?: string;
	compactedTurnCount: number;
	keptTurnCount: number;
	tokensBefore: number;
	message: PersistedKitAgentMessage;
	keptTurns: Turn[];
}

export type SessionEntry =
	| SessionMessageEntry
	| SessionInfoEntry
	| SessionModelChangeEntry
	| SessionThinkingLevelChangeEntry
	| SessionCwdChangeEntry
	| SessionCompactionEntry
	| SessionHandoffSummaryEntry
	| SubagentStartedEntry
	| SubagentPromptEntry
	| SubagentMessageStartedEntry
	| SubagentMessageDeltaEntry
	| SubagentMessageCompletedEntry
	| SubagentThinkingStartedEntry
	| SubagentThinkingDeltaEntry
	| SubagentThinkingCompletedEntry
	| SubagentToolStartedEntry
	| SubagentToolUpdatedEntry
	| SubagentToolCompletedEntry
	| SubagentDismissedEntry
	| SubagentFailedEntry
	| SubagentAbortedEntry
	| SubagentCompactionEntry;

export type SessionFileEntry = SessionHeader | SessionEntry;
