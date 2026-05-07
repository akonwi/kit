/**
 * Session file storage — read/write ~/.kit/sessions/<id>.jsonl
 */

import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import {
	appendFile,
	mkdir,
	readdir,
	readFile,
	rm,
	writeFile,
} from "node:fs/promises";
import path from "node:path";
import { getKitPaths } from "../paths";
import {
	type KitAgentMessage,
	type PersistedKitAgentMessage,
	SESSION_VERSION,
	type Session,
	type SessionCompactionEntry,
	type SessionEntry,
	type SessionFileEntry,
	type SessionHandoffSummaryEntry,
	type SessionHeader,
	type SessionInfoEntry,
	type SessionMessageEntry,
	type SessionModelChangeEntry,
	type SessionSummary,
	type SessionThinkingLevelChangeEntry,
	type SubagentAbortedEntry,
	type SubagentFailedEntry,
	type SubagentPromptEntry,
	type SubagentStartedEntry,
	type Turn,
} from "./types";

export type AppendableSessionEntry = SessionEntry extends infer T
	? T extends SessionEntry
		? Omit<T, "id" | "parentId">
		: never
	: never;

export const SESSIONS_DIR = path.join(getKitPaths().kitRoot, "sessions");

type LegacySession = Session;

type SessionStorageState = {
	header: SessionHeader;
	entries: SessionEntry[];
	filePath: string;
	legacyFilePath: string;
	flushedEntryCount: number;
	firstEntryIdByTurnId: Map<string, string>;
	name?: string;
	model?: string;
	thinkingLevel?: Session["thinkingLevel"];
};

const stateBySessionId = new Map<string, SessionStorageState>();
const writeChains = new Map<string, Promise<void>>();

function sessionsDir(): string {
	return path.join(getKitPaths().kitRoot, "sessions");
}

function sessionPath(id: string): string {
	return path.join(sessionsDir(), `${id}.jsonl`);
}

function legacySessionPath(id: string): string {
	return path.join(sessionsDir(), `${id}.json`);
}

async function ensureSessionsDir(): Promise<void> {
	await mkdir(sessionsDir(), { recursive: true });
}

function now(): string {
	return new Date().toISOString();
}

function toIsoTimestamp(
	value: string | number | undefined,
	fallback: string,
): string {
	if (typeof value === "string") return value;
	if (typeof value === "number" && Number.isFinite(value)) {
		return new Date(value).toISOString();
	}
	return fallback;
}

function makeEntryId(): string {
	return randomUUID();
}

function stripTurnId(message: KitAgentMessage): PersistedKitAgentMessage {
	const { turnId: _turnId, ...rest } = message;
	return rest;
}

function restoreMessage(
	turnId: string,
	message: PersistedKitAgentMessage,
): KitAgentMessage {
	return {
		...message,
		turnId,
	};
}

function isSummaryTurn(
	turn: Turn,
	kind: NonNullable<KitAgentMessage["synthetic"]>["kind"],
): boolean {
	if (turn.messages.length !== 1) return false;
	const message = turn.messages[0];
	return message.role === "assistant" && message.synthetic?.kind === kind;
}

function summaryTurnFromPersistedMessage(
	entryId: string,
	message: PersistedKitAgentMessage,
): Turn {
	return {
		id: entryId,
		messages: [restoreMessage(entryId, message)],
	};
}

function subagentDelegationSignature(
	agentName: string,
	message: string,
): string {
	return `${agentName}\u0000${message}`;
}

function countRealSubagentRunToolCalls(
	entries: SessionEntry[],
): Map<string, number> {
	const counts = new Map<string, number>();
	for (const entry of entries) {
		if (entry.type !== "message" || entry.message.role !== "assistant")
			continue;
		for (const block of entry.message.content) {
			if (block.type !== "toolCall" || block.name !== "subagent") continue;
			const args =
				typeof block.arguments === "object" && block.arguments !== null
					? (block.arguments as Record<string, unknown>)
					: null;
			if (!args || args.action !== "run") continue;
			if (typeof args.agent !== "string" || typeof args.message !== "string") {
				continue;
			}
			const signature = subagentDelegationSignature(args.agent, args.message);
			counts.set(signature, (counts.get(signature) ?? 0) + 1);
		}
	}
	return counts;
}

function consumeRealSubagentRunToolCall(
	counts: Map<string, number>,
	prompt: SubagentPromptEntry,
): boolean {
	const signature = subagentDelegationSignature(
		prompt.agentName,
		prompt.prompt,
	);
	const count = counts.get(signature) ?? 0;
	if (count <= 0) return false;
	if (count === 1) counts.delete(signature);
	else counts.set(signature, count - 1);
	return true;
}

function subagentDelegationTurnFromEntries(
	prompt: SubagentPromptEntry,
	started?: SubagentStartedEntry,
): Turn {
	const timestamp = new Date(prompt.timestamp).getTime();
	const toolCallId = `subagent:${prompt.id}`;
	return {
		id: prompt.id,
		messages: [
			{
				role: "assistant",
				content: [
					{
						type: "toolCall",
						id: toolCallId,
						name: "subagent",
						arguments: {
							action: "run",
							agent: prompt.agentName,
							message: prompt.prompt,
						},
					},
				],
				api: "openai-completions",
				provider: "kit",
				model: started?.model ?? "subagent",
				usage: {
					input: 0,
					output: 0,
					cacheRead: 0,
					cacheWrite: 0,
					totalTokens: 0,
					cost: {
						input: 0,
						output: 0,
						cacheRead: 0,
						cacheWrite: 0,
						total: 0,
					},
				},
				stopReason: "toolUse",
				timestamp,
				synthetic: {
					kind: "subagent-delegation",
					subagentName: prompt.agentName,
					subagentDescription: started?.description,
					subagentPrompt: prompt.prompt,
					subagentSource: prompt.source,
				},
				turnId: prompt.id,
			} as KitAgentMessage,
			{
				role: "toolResult",
				toolCallId,
				toolName: "subagent",
				content: [],
				isError: false,
				timestamp,
				turnId: prompt.id,
			} as KitAgentMessage,
		],
	};
}

function setSubagentDelegationTurnFailure(
	turn: Turn,
	failure: SubagentFailedEntry,
): void {
	const assistant = turn.messages[0];
	const toolResult = turn.messages[1];
	if (assistant?.role !== "assistant" || toolResult?.role !== "toolResult") {
		return;
	}
	toolResult.isError = true;
	toolResult.timestamp = new Date(failure.timestamp).getTime();
	toolResult.content = [{ type: "text", text: failure.error }];
}

function setSubagentDelegationTurnAborted(
	turn: Turn,
	aborted: SubagentAbortedEntry,
): void {
	const assistant = turn.messages[0];
	const toolResult = turn.messages[1];
	if (assistant?.role !== "assistant" || toolResult?.role !== "toolResult") {
		return;
	}
	assistant.stopReason = "aborted";
	assistant.timestamp = new Date(aborted.timestamp).getTime();
	toolResult.timestamp = new Date(aborted.timestamp).getTime();
	toolResult.content = aborted.reason
		? [{ type: "text", text: aborted.reason }]
		: [];
}

function firstUserMessage(turns: Turn[]): string | undefined {
	for (const turn of turns) {
		for (const msg of turn.messages) {
			if (msg.role !== "user") continue;
			const content = (msg as { content?: unknown }).content;
			if (typeof content === "string") return content.slice(0, 120);
			if (Array.isArray(content)) {
				for (const block of content) {
					if (
						block &&
						typeof block === "object" &&
						"type" in block &&
						(block as { type: string }).type === "text" &&
						"text" in block
					) {
						return (block as { text: string }).text.slice(0, 120);
					}
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
		parentSessionId: session.parentSessionId,
		forkedFromTurnId: session.forkedFromTurnId,
		name: session.name,
		model: session.model,
		thinkingLevel: session.thinkingLevel,
		createdAt: session.createdAt,
		updatedAt: session.updatedAt,
		messageCount: session.turns.reduce(
			(count, turn) => count + turn.messages.length,
			0,
		),
		firstMessage: firstUserMessage(session.turns),
	};
}

type PendingSerializedEntry =
	| {
			kind: "message";
			id: string;
			turnId: string;
			message: PersistedKitAgentMessage;
			timestamp: string;
	  }
	| {
			kind: "compaction";
			id: string;
			message: PersistedKitAgentMessage;
			timestamp: string;
			compactedTurnCount: number;
			keptTurnCount: number;
			tokensBefore: number;
	  }
	| {
			kind: "handoff_summary";
			id: string;
			message: PersistedKitAgentMessage;
			timestamp: string;
	  };

function serializeSessionEntries(session: Session): SessionEntry[] {
	const pending: PendingSerializedEntry[] = [];

	for (const turn of session.turns) {
		if (isSummaryTurn(turn, "compaction-summary")) {
			const message = turn.messages[0];
			pending.push({
				kind: "compaction",
				id: makeEntryId(),
				message: stripTurnId(message),
				timestamp: toIsoTimestamp(message.timestamp, session.updatedAt),
				compactedTurnCount: 0,
				keptTurnCount: Math.max(session.turns.length - 1, 0),
				tokensBefore: 0,
			});
			continue;
		}
		if (isSummaryTurn(turn, "handoff-summary")) {
			const message = turn.messages[0];
			pending.push({
				kind: "handoff_summary",
				id: makeEntryId(),
				message: stripTurnId(message),
				timestamp: toIsoTimestamp(message.timestamp, session.updatedAt),
			});
			continue;
		}
		if (isSummaryTurn(turn, "subagent-delegation")) {
			continue;
		}
		for (const message of turn.messages) {
			pending.push({
				kind: "message",
				id: makeEntryId(),
				turnId: turn.id,
				message: stripTurnId(message),
				timestamp: toIsoTimestamp(message.timestamp, session.updatedAt),
			});
		}
	}

	const entries: SessionEntry[] = [];
	let parentId: string | null = null;
	for (let index = 0; index < pending.length; index++) {
		const item = pending[index];
		if (item.kind === "message") {
			const entry: SessionMessageEntry = {
				type: "message",
				id: item.id,
				parentId,
				timestamp: item.timestamp,
				turnId: item.turnId,
				message: item.message,
			};
			entries.push(entry);
			parentId = entry.id;
			continue;
		}
		if (item.kind === "compaction") {
			const next = pending[index + 1];
			const entry: SessionCompactionEntry = {
				type: "compaction",
				id: item.id,
				parentId,
				timestamp: item.timestamp,
				firstKeptEntryId: next?.id,
				compactedTurnCount: item.compactedTurnCount,
				keptTurnCount: item.keptTurnCount,
				tokensBefore: item.tokensBefore,
				message: item.message,
			};
			entries.push(entry);
			parentId = entry.id;
			continue;
		}
		const entry: SessionHandoffSummaryEntry = {
			type: "handoff_summary",
			id: item.id,
			parentId,
			timestamp: item.timestamp,
			message: item.message,
		};
		entries.push(entry);
		parentId = entry.id;
	}

	return entries;
}

function buildHeader(session: Session): SessionHeader {
	return {
		type: "session",
		version: SESSION_VERSION,
		id: session.id,
		createdAt: session.createdAt,
		cwd: session.cwd,
		parentSessionId: session.parentSessionId,
		forkedFromTurnId: session.forkedFromTurnId,
		name: session.name,
		model: session.model,
		thinkingLevel: session.thinkingLevel,
	};
}

function buildState(
	header: SessionHeader,
	entries: SessionEntry[],
	filePath: string,
	legacyFilePath: string,
	flushedEntryCount: number,
): SessionStorageState {
	const firstEntryIdByTurnId = new Map<string, string>();
	let name = header.name?.trim() || undefined;
	let model = header.model;
	let thinkingLevel = header.thinkingLevel;

	for (const entry of entries) {
		if (entry.type === "message") {
			if (!firstEntryIdByTurnId.has(entry.turnId)) {
				firstEntryIdByTurnId.set(entry.turnId, entry.id);
			}
			if (entry.message.role === "assistant" && "model" in entry.message) {
				model = entry.message.model;
			}
			continue;
		}
		if (entry.type === "session_info") {
			name = entry.name?.trim() || undefined;
			continue;
		}
		if (entry.type === "model_change") {
			model = entry.modelId;
			continue;
		}
		if (entry.type === "thinking_level_change") {
			thinkingLevel = entry.thinkingLevel;
		}
	}

	return {
		header,
		entries,
		filePath,
		legacyFilePath,
		flushedEntryCount,
		firstEntryIdByTurnId,
		name,
		model,
		thinkingLevel,
	};
}

function buildSessionFromState(state: SessionStorageState): Session {
	const latestCompactionIndex = state.entries.findLastIndex(
		(entry) => entry.type === "compaction",
	);
	const latestCompaction =
		latestCompactionIndex >= 0
			? (state.entries[latestCompactionIndex] as SessionCompactionEntry)
			: null;

	let visibleEntries = state.entries;
	if (latestCompaction) {
		const boundaryIndex = latestCompaction.firstKeptEntryId
			? state.entries.findIndex(
					(entry) => entry.id === latestCompaction.firstKeptEntryId,
				)
			: -1;
		visibleEntries =
			boundaryIndex >= 0 ? state.entries.slice(boundaryIndex) : [];
	}

	const turns: Turn[] = [];
	if (latestCompaction) {
		turns.push(
			summaryTurnFromPersistedMessage(
				latestCompaction.id,
				latestCompaction.message,
			),
		);
	}

	const subagentStartsByConversation = new Map<string, SubagentStartedEntry>();
	const realSubagentRunToolCalls =
		countRealSubagentRunToolCalls(visibleEntries);
	const syntheticDelegationTurnByConversation = new Map<string, Turn>();

	for (const entry of visibleEntries) {
		if (latestCompaction && entry.id === latestCompaction.id) continue;
		if (entry.type === "message") {
			const restored = restoreMessage(entry.turnId, entry.message);
			const lastTurn = turns.at(-1);
			if (lastTurn && lastTurn.id === entry.turnId) {
				lastTurn.messages.push(restored);
			} else {
				turns.push({ id: entry.turnId, messages: [restored] });
			}
			continue;
		}
		if (entry.type === "subagent_started") {
			subagentStartsByConversation.set(entry.subagentConversationId, entry);
			continue;
		}
		if (entry.type === "subagent_prompt") {
			if (consumeRealSubagentRunToolCall(realSubagentRunToolCalls, entry)) {
				syntheticDelegationTurnByConversation.delete(
					entry.subagentConversationId,
				);
				continue;
			}
			const turn = subagentDelegationTurnFromEntries(
				entry,
				subagentStartsByConversation.get(entry.subagentConversationId),
			);
			turns.push(turn);
			syntheticDelegationTurnByConversation.set(
				entry.subagentConversationId,
				turn,
			);
			continue;
		}
		if (entry.type === "subagent_failed") {
			const turn = syntheticDelegationTurnByConversation.get(
				entry.subagentConversationId,
			);
			if (turn) setSubagentDelegationTurnFailure(turn, entry);
			continue;
		}
		if (entry.type === "subagent_aborted") {
			const turn = syntheticDelegationTurnByConversation.get(
				entry.subagentConversationId,
			);
			if (turn) setSubagentDelegationTurnAborted(turn, entry);
			continue;
		}
		if (entry.type === "subagent_dismissed") {
			syntheticDelegationTurnByConversation.delete(
				entry.subagentConversationId,
			);
			continue;
		}
		if (entry.type === "handoff_summary" || entry.type === "compaction") {
			turns.push(summaryTurnFromPersistedMessage(entry.id, entry.message));
		}
	}

	const updatedAt = state.entries.at(-1)?.timestamp ?? state.header.createdAt;

	return {
		id: state.header.id,
		version: SESSION_VERSION,
		cwd: state.header.cwd,
		parentSessionId: state.header.parentSessionId,
		forkedFromTurnId: state.header.forkedFromTurnId,
		name: state.name,
		model: state.model,
		thinkingLevel: state.thinkingLevel,
		createdAt: state.header.createdAt,
		updatedAt,
		turns,
	};
}

function serializeFile(state: SessionStorageState): string {
	return [state.header, ...state.entries]
		.map((entry) => JSON.stringify(entry))
		.join("\n")
		.concat("\n");
}

async function flushStateToDisk(state: SessionStorageState): Promise<void> {
	await ensureSessionsDir();
	const pendingEntries = state.entries.slice(state.flushedEntryCount);
	if (pendingEntries.length === 0 && state.flushedEntryCount > 0) {
		return;
	}
	if (state.flushedEntryCount === 0) {
		await writeFile(state.filePath, serializeFile(state), "utf8");
		state.flushedEntryCount = state.entries.length;
	} else if (pendingEntries.length > 0) {
		await appendFile(
			state.filePath,
			pendingEntries
				.map((entry) => JSON.stringify(entry))
				.join("\n")
				.concat("\n"),
			"utf8",
		);
		state.flushedEntryCount = state.entries.length;
	}
	if (existsSync(state.legacyFilePath)) {
		await rm(state.legacyFilePath, { force: true }).catch(() => {});
	}
}

function applyEntryToState(
	state: SessionStorageState,
	entry: SessionEntry,
): void {
	state.entries.push(entry);
	if (entry.type === "message") {
		if (!state.firstEntryIdByTurnId.has(entry.turnId)) {
			state.firstEntryIdByTurnId.set(entry.turnId, entry.id);
		}
		if (entry.message.role === "assistant" && "model" in entry.message) {
			state.model = entry.message.model;
		}
		return;
	}
	if (entry.type === "session_info") {
		state.name = entry.name?.trim() || undefined;
		return;
	}
	if (entry.type === "model_change") {
		state.model = entry.modelId;
		return;
	}
	if (entry.type === "thinking_level_change") {
		state.thinkingLevel = entry.thinkingLevel;
	}
}

type SessionStorageSnapshot = Pick<
	SessionStorageState,
	"flushedEntryCount" | "name" | "model" | "thinkingLevel"
> & {
	entryCount: number;
	firstEntryIdByTurnId: Map<string, string>;
};

function snapshotState(state: SessionStorageState): SessionStorageSnapshot {
	return {
		entryCount: state.entries.length,
		flushedEntryCount: state.flushedEntryCount,
		firstEntryIdByTurnId: new Map(state.firstEntryIdByTurnId),
		name: state.name,
		model: state.model,
		thinkingLevel: state.thinkingLevel,
	};
}

function restoreState(
	state: SessionStorageState,
	snapshot: SessionStorageSnapshot,
): void {
	state.entries.length = snapshot.entryCount;
	state.flushedEntryCount = snapshot.flushedEntryCount;
	state.firstEntryIdByTurnId = snapshot.firstEntryIdByTurnId;
	state.name = snapshot.name;
	state.model = snapshot.model;
	state.thinkingLevel = snapshot.thinkingLevel;
}

type PreparedAppend<T> = {
	entries: SessionEntry[];
	result: T;
};

async function appendEntries<T>(
	state: SessionStorageState,
	prepare: () => PreparedAppend<T> | Promise<PreparedAppend<T>>,
): Promise<T> {
	const existing = writeChains.get(state.header.id) ?? Promise.resolve();
	const next = existing.then(async () => {
		const snapshot = snapshotState(state);
		try {
			const { entries, result } = await prepare();
			if (entries.length === 0) return result;
			for (const entry of entries) {
				applyEntryToState(state, entry);
			}
			await flushStateToDisk(state);
			return result;
		} catch (error) {
			restoreState(state, snapshot);
			throw error;
		}
	});
	writeChains.set(
		state.header.id,
		next.then(
			() => {},
			() => {},
		),
	);
	return next;
}

function createStateFromSession(
	session: Session,
	options?: { flushed?: boolean },
): SessionStorageState {
	const header = buildHeader(session);
	const entries = serializeSessionEntries(session);
	return buildState(
		header,
		entries,
		sessionPath(session.id),
		legacySessionPath(session.id),
		options?.flushed ? entries.length : 0,
	);
}

function parseJsonl(content: string): SessionFileEntry[] {
	const lines = content
		.split("\n")
		.map((line) => line.trim())
		.filter((line) => line.length > 0);
	const entries: SessionFileEntry[] = [];
	for (const line of lines) {
		try {
			entries.push(JSON.parse(line) as SessionFileEntry);
		} catch {
			// Ignore malformed lines.
		}
	}
	return entries;
}

async function loadJsonlState(id: string): Promise<SessionStorageState | null> {
	const filePath = sessionPath(id);
	if (!existsSync(filePath)) return null;
	try {
		const raw = await readFile(filePath, "utf8");
		const parsed = parseJsonl(raw);
		const [header, ...entries] = parsed;
		if (!header || header.type !== "session") return null;
		const state = buildState(
			header,
			entries.filter(
				(entry): entry is SessionEntry => entry.type !== "session",
			),
			filePath,
			legacySessionPath(id),
			entries.length,
		);
		stateBySessionId.set(id, state);
		return state;
	} catch {
		return null;
	}
}

async function migrateLegacySession(id: string): Promise<Session | null> {
	const filePath = legacySessionPath(id);
	if (!existsSync(filePath)) return null;
	try {
		const raw = await readFile(filePath, "utf8");
		const session = JSON.parse(raw) as LegacySession;
		if (!session || typeof session.id !== "string") return null;
		const normalized: Session = {
			...session,
			version: SESSION_VERSION,
		};
		await writeSession(normalized);
		return normalized;
	} catch {
		return null;
	}
}

async function ensureState(id: string): Promise<SessionStorageState | null> {
	const existing = stateBySessionId.get(id);
	if (existing) return existing;
	return (await loadJsonlState(id)) ?? null;
}

// --- CRUD ---

export async function createSession(
	cwd: string,
	model?: string,
	thinkingLevel?: Session["thinkingLevel"],
): Promise<Session> {
	const timestamp = now();
	const session: Session = {
		id: randomUUID(),
		version: SESSION_VERSION,
		cwd,
		model,
		thinkingLevel,
		createdAt: timestamp,
		updatedAt: timestamp,
		turns: [],
	};
	stateBySessionId.set(
		session.id,
		buildState(
			buildHeader(session),
			[],
			sessionPath(session.id),
			legacySessionPath(session.id),
			0,
		),
	);
	return session;
}

export async function readSession(id: string): Promise<Session | null> {
	const state = await ensureState(id);
	if (state) return buildSessionFromState(state);
	return migrateLegacySession(id);
}

export async function readSessionEntries(id: string): Promise<SessionEntry[]> {
	const state = await ensureState(id);
	if (state) return [...state.entries];
	const migrated = await migrateLegacySession(id);
	if (!migrated) return [];
	const migratedState = await ensureState(id);
	return migratedState ? [...migratedState.entries] : [];
}

export async function writeSession(session: Session): Promise<void> {
	const state = createStateFromSession(session, { flushed: true });
	await ensureSessionsDir();
	await writeFile(state.filePath, serializeFile(state), "utf8");
	if (existsSync(state.legacyFilePath)) {
		await rm(state.legacyFilePath, { force: true });
	}
	stateBySessionId.set(session.id, state);
}

export async function appendSessionEntries(
	session: Session,
	entries: AppendableSessionEntry[],
): Promise<SessionEntry[]> {
	let state = await ensureState(session.id);
	if (!state) {
		state = createStateFromSession({ ...session, turns: [] });
		stateBySessionId.set(session.id, state);
	}
	return appendEntries(state, () => {
		const prepared: SessionEntry[] = [];
		let parentId = state.entries.at(-1)?.id ?? null;
		for (const entry of entries) {
			const next = {
				...entry,
				id: makeEntryId(),
				parentId,
			} as SessionEntry;
			prepared.push(next);
			parentId = next.id;
		}
		return { entries: prepared, result: prepared };
	});
}

export async function appendMessage(
	session: Session,
	turnId: string,
	message: KitAgentMessage,
): Promise<void> {
	let state = await ensureState(session.id);
	if (!state) {
		state = createStateFromSession({ ...session, turns: [] });
		stateBySessionId.set(session.id, state);
	}
	await appendEntries(state, () => {
		const entry: SessionMessageEntry = {
			type: "message",
			id: makeEntryId(),
			parentId: state.entries.at(-1)?.id ?? null,
			timestamp: toIsoTimestamp(message.timestamp, session.updatedAt),
			turnId,
			message: stripTurnId(message),
		};
		return { entries: [entry], result: undefined };
	});
}

export async function appendTurn(session: Session, turn: Turn): Promise<void> {
	let state = await ensureState(session.id);
	if (!state) {
		state = createStateFromSession({ ...session, turns: [] });
		stateBySessionId.set(session.id, state);
	}
	await appendEntries(state, () => {
		if (state.firstEntryIdByTurnId.has(turn.id)) {
			return { entries: [], result: undefined };
		}

		const entries: SessionMessageEntry[] = [];
		let parentId = state.entries.at(-1)?.id ?? null;
		for (const message of turn.messages) {
			const entry: SessionMessageEntry = {
				type: "message",
				id: makeEntryId(),
				parentId,
				timestamp: toIsoTimestamp(message.timestamp, session.updatedAt),
				turnId: turn.id,
				message: stripTurnId(message),
			};
			entries.push(entry);
			parentId = entry.id;
		}
		return { entries, result: undefined };
	});
}

export async function appendSessionInfo(
	session: Session,
	name?: string,
): Promise<void> {
	let state = await ensureState(session.id);
	if (!state) {
		state = createStateFromSession(session);
		stateBySessionId.set(session.id, state);
	}
	await appendEntries(state, () => {
		const normalized = name?.trim() || undefined;
		if (state.name === normalized) return { entries: [], result: undefined };
		const entry: SessionInfoEntry = {
			type: "session_info",
			id: makeEntryId(),
			parentId: state.entries.at(-1)?.id ?? null,
			timestamp: session.updatedAt,
			name: normalized,
		};
		return { entries: [entry], result: undefined };
	});
}

export async function appendModelChange(session: Session): Promise<void> {
	let state = await ensureState(session.id);
	if (!state) {
		state = createStateFromSession(session);
		stateBySessionId.set(session.id, state);
	}
	await appendEntries(state, () => {
		if (state.model === session.model) {
			return { entries: [], result: undefined };
		}
		const entry: SessionModelChangeEntry = {
			type: "model_change",
			id: makeEntryId(),
			parentId: state.entries.at(-1)?.id ?? null,
			timestamp: session.updatedAt,
			modelId: session.model,
		};
		return { entries: [entry], result: undefined };
	});
}

export async function appendThinkingLevelChange(
	session: Session,
): Promise<void> {
	let state = await ensureState(session.id);
	if (!state) {
		state = createStateFromSession(session);
		stateBySessionId.set(session.id, state);
	}
	await appendEntries(state, () => {
		if (state.thinkingLevel === session.thinkingLevel) {
			return { entries: [], result: undefined };
		}
		const entry: SessionThinkingLevelChangeEntry = {
			type: "thinking_level_change",
			id: makeEntryId(),
			parentId: state.entries.at(-1)?.id ?? null,
			timestamp: session.updatedAt,
			thinkingLevel: session.thinkingLevel,
		};
		return { entries: [entry], result: undefined };
	});
}

export async function appendCompaction(options: {
	session: Session;
	summaryMessage: Extract<KitAgentMessage, { role: "assistant" }>;
	firstKeptTurnId?: string;
	compactedTurnCount: number;
	keptTurnCount: number;
	tokensBefore: number;
}): Promise<void> {
	const {
		session,
		summaryMessage,
		firstKeptTurnId,
		compactedTurnCount,
		keptTurnCount,
		tokensBefore,
	} = options;
	let state = await ensureState(session.id);
	if (!state) {
		state = createStateFromSession(session);
		stateBySessionId.set(session.id, state);
	}
	await appendEntries(state, () => {
		const firstKeptEntryId = firstKeptTurnId
			? state.firstEntryIdByTurnId.get(firstKeptTurnId)
			: undefined;
		if (firstKeptTurnId && !firstKeptEntryId) {
			throw new Error(
				`Compaction boundary could not be resolved for kept turn ${firstKeptTurnId}. Persist kept turns before appending compaction.`,
			);
		}
		const entry: SessionCompactionEntry = {
			type: "compaction",
			id: makeEntryId(),
			parentId: state.entries.at(-1)?.id ?? null,
			timestamp: toIsoTimestamp(summaryMessage.timestamp, session.updatedAt),
			firstKeptEntryId,
			compactedTurnCount,
			keptTurnCount,
			tokensBefore,
			message: stripTurnId(summaryMessage),
		};
		return { entries: [entry], result: undefined };
	});
}

export async function appendHandoffSummary(
	session: Session,
	summaryMessage: Extract<KitAgentMessage, { role: "assistant" }>,
): Promise<void> {
	let state = await ensureState(session.id);
	if (!state) {
		state = createStateFromSession(session);
		stateBySessionId.set(session.id, state);
	}
	await appendEntries(state, () => {
		const entry: SessionHandoffSummaryEntry = {
			type: "handoff_summary",
			id: makeEntryId(),
			parentId: state.entries.at(-1)?.id ?? null,
			timestamp: toIsoTimestamp(summaryMessage.timestamp, session.updatedAt),
			message: stripTurnId(summaryMessage),
		};
		return { entries: [entry], result: undefined };
	});
}

// persisting to disk should be a side-effect
export async function updateSession(
	session: Session,
	changes: Partial<Pick<Session, "name" | "model" | "thinkingLevel" | "turns">>,
): Promise<Session> {
	const updated: Session = {
		...session,
		...changes,
		updatedAt: now(),
	};
	if (changes.turns) {
		await writeSession(updated);
		return updated;
	}
	if (Object.hasOwn(changes, "name")) {
		await appendSessionInfo(updated, updated.name);
	}
	if (Object.hasOwn(changes, "model")) {
		await appendModelChange(updated);
	}
	if (Object.hasOwn(changes, "thinkingLevel")) {
		await appendThinkingLevelChange(updated);
	}
	return updated;
}

export async function deleteSession(id: string): Promise<void> {
	await rm(sessionPath(id), { force: true });
	await rm(legacySessionPath(id), { force: true });
	stateBySessionId.delete(id);
	writeChains.delete(id);
}

// --- Listing ---

async function readLegacySummary(id: string): Promise<SessionSummary | null> {
	const filePath = legacySessionPath(id);
	if (!existsSync(filePath)) return null;
	try {
		const raw = await readFile(filePath, "utf8");
		const session = JSON.parse(raw) as LegacySession;
		if (!session || typeof session.id !== "string") return null;
		return toSummary({ ...session, version: SESSION_VERSION });
	} catch {
		return null;
	}
}

async function readSummary(id: string): Promise<SessionSummary | null> {
	const state = (await loadJsonlState(id)) ?? null;
	if (state) return toSummary(buildSessionFromState(state));
	return readLegacySummary(id);
}

export async function listAllSessions(): Promise<SessionSummary[]> {
	const dir = sessionsDir();
	if (!existsSync(dir)) return [];

	const ids = new Set<string>();
	for (const file of await readdir(dir)) {
		if (file.endsWith(".jsonl")) ids.add(file.replace(/\.jsonl$/, ""));
		if (file.endsWith(".json")) ids.add(file.replace(/\.json$/, ""));
	}

	const summaries = (
		await Promise.all(Array.from(ids, (id) => readSummary(id)))
	).filter((summary): summary is SessionSummary => summary !== null);

	return summaries.sort(
		(a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
	);
}

export async function listSessionsForCwd(
	cwd: string,
): Promise<SessionSummary[]> {
	const all = await listAllSessions();
	return all.filter((s) => s.cwd === cwd);
}

export async function findSessionById(
	idPrefix: string,
): Promise<Session | null> {
	const dir = sessionsDir();
	if (!existsSync(dir)) return null;

	const needle = idPrefix.toLowerCase();
	const files = await readdir(dir);
	const ids = Array.from(
		new Set(
			files
				.filter((file) => file.endsWith(".jsonl") || file.endsWith(".json"))
				.map((file) => file.replace(/\.(jsonl|json)$/, "")),
		),
	);

	for (const id of ids) {
		if (id.toLowerCase().startsWith(needle)) {
			return readSession(id);
		}
	}

	return null;
}

/** Open the most recent session for cwd, or create a new one. */
export async function openRecentSession(
	cwd: string,
	model?: string,
): Promise<Session> {
	const sessions = await listSessionsForCwd(cwd);
	if (sessions.length > 0) {
		const session = await readSession(sessions[0].id);
		if (session) return session;
	}
	return createSession(cwd, model);
}
