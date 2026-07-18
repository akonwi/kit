import { randomUUID } from "node:crypto";
import type { ImageContent, TextContent } from "../../runtime/agent";
import {
	AgentRuntime,
	type AgentRuntimeEvent,
} from "../../runtime/agent-runtime";
import {
	type AppendableSessionEntry,
	appendSessionEntries,
	type PersistedKitAgentMessage,
	readSessionEntries,
	SESSION_VERSION,
	type Session,
	type SessionEntry,
	type SubagentAbortedEntry,
	type SubagentEventSource,
	type SubagentFailedEntry,
	type Turn,
} from "../../session";
import {
	appendSubagentSessionEntries,
	createSubagentSession,
	deleteSubagentSession,
	readSubagentSessionEntries,
	readSubagentSessionHeader,
} from "../../storage/subagent-session-storage";
import type { SubagentDefinition } from "./discovery";

export type ActiveSubagentStatus = "idle" | "running" | "failed" | "aborted";

export interface LiveSubagentRuntime {
	run(prompt: string): Promise<SubagentRunResult>;
	abort(reason?: string): void;
	dispose(): void;
}

export interface ActiveSubagentConversationState {
	agentName: string;
	subagentConversationId: string;
	status: ActiveSubagentStatus;
	model?: string;
	description?: string;
	lastActivityAt: string;
	latestMessage?: string;
	failureMessage?: string;
	abortReason?: string;
	runtime?: LiveSubagentRuntime;
	initializing?: Promise<void>;
}

export interface SubagentRunResult {
	status: "completed" | "failed" | "aborted";
	message?: string;
	error?: string;
}

export class SubagentManagerError extends Error {
	constructor(
		readonly code:
			| "SUBAGENT_NOT_FOUND"
			| "SUBAGENT_BUSY"
			| "INVALID_INPUT"
			| "RUNTIME_ERROR",
		message: string,
	) {
		super(message);
		this.name = "SubagentManagerError";
	}
}

type RuntimeLike = {
	agentInfo: {
		model: AgentRuntime["agentInfo"]["model"] | undefined;
		thinkingLevel: AgentRuntime["agentInfo"]["thinkingLevel"];
	};
	getAvailableModels: AgentRuntime["getAvailableModels"];
	getContextFiles: AgentRuntime["getContextFiles"];
	getSession: AgentRuntime["getSession"];
	getSystemPromptAdditions: AgentRuntime["getSystemPromptAdditions"];
	getTools: AgentRuntime["getTools"];
	settings: AgentRuntime["settings"];
};

interface SubagentExecutorFactoryOptions {
	runtime: RuntimeLike;
	definition: SubagentDefinition;
	historyTurns: Turn[];
	onEntries: (entries: AppendableSessionEntry[]) => Promise<void>;
	onCompletedMessage: (message: PersistedKitAgentMessage, text: string) => void;
	onTerminalState: (
		status: ActiveSubagentStatus,
		options?: { error?: string; reason?: string },
	) => void;
	subagentConversationId: string;
}

export type SubagentSessionStorage = {
	appendEntries: typeof appendSubagentSessionEntries;
	create: typeof createSubagentSession;
	delete: typeof deleteSubagentSession;
	readEntries: typeof readSubagentSessionEntries;
	readHeader: typeof readSubagentSessionHeader;
};

interface SubagentManagerOptions {
	runtime: RuntimeLike;
	getAgents: () => SubagentDefinition[];
	appendEntries?: typeof appendSessionEntries;
	readEntries?: typeof readSessionEntries;
	subagentStorage?: SubagentSessionStorage;
	createRuntime?: (
		options: SubagentExecutorFactoryOptions,
	) => Promise<LiveSubagentRuntime> | LiveSubagentRuntime;
}

function nowIso(): string {
	return new Date().toISOString();
}

function trimToUndefined(value: string | undefined): string | undefined {
	const trimmed = value?.trim();
	return trimmed ? trimmed : undefined;
}

function stripTurnId(
	message: PersistedKitAgentMessage & { turnId?: string },
): PersistedKitAgentMessage {
	const { turnId: _turnId, ...rest } = message;
	return rest;
}

function extractAssistantText(message: PersistedKitAgentMessage): string {
	if (message.role !== "assistant") return "";
	return message.content
		.filter(
			(block): block is { type: "text"; text: string } =>
				block.type === "text" && typeof block.text === "string",
		)
		.map((block) => block.text)
		.join("\n\n")
		.trim();
}

function assistantMessageId(message: unknown): string | undefined {
	if (!message || typeof message !== "object" || !("id" in message)) {
		return undefined;
	}
	const id = message.id;
	return typeof id === "string" && id.trim().length > 0 ? id : undefined;
}

function normalizeToolResultContent(result: unknown): {
	content: (TextContent | ImageContent)[];
	details?: unknown;
} {
	if (result && typeof result === "object") {
		const candidate = result as { content?: unknown; details?: unknown };
		if (Array.isArray(candidate.content)) {
			const content = candidate.content.filter(
				(block): block is TextContent | ImageContent =>
					Boolean(block) &&
					typeof block === "object" &&
					"type" in block &&
					(((block as { type?: unknown }).type === "text" &&
						typeof (block as { text?: unknown }).text === "string") ||
						(block as { type?: unknown }).type === "image"),
			);
			if (content.length > 0) {
				return { content, details: candidate.details };
			}
		}
	}

	const text =
		typeof result === "string"
			? result
			: result === undefined
				? ""
				: (JSON.stringify(result, null, 2) ?? String(result));
	return {
		content: text.trim().length > 0 ? [{ type: "text", text }] : [],
		details:
			result && typeof result === "object" && "details" in result
				? (result as { details?: unknown }).details
				: undefined,
	};
}

function normalizeTurn(turn: Turn): Turn {
	return {
		...turn,
		messages: turn.messages.map((message) => ({
			...message,
			turnId: turn.id,
		})),
	};
}

type RuntimeCompactionEvent = Extract<
	AgentRuntimeEvent,
	{
		type:
			| "session.compaction.completed.auto"
			| "session.compaction.completed.recovery"
			| "session.compaction.completed.adaptation"
			| "session.compaction.completed.manual";
	}
>;

type SubagentEntryMetadata = {
	timestamp: string;
	agentName: string;
	subagentConversationId: string;
};

export function createSubagentCompactionEntry(
	event: RuntimeCompactionEvent,
	baseEntry: SubagentEntryMetadata,
): AppendableSessionEntry {
	return {
		...baseEntry,
		type: "subagent_compaction",
		message: stripTurnId(event.summaryMessage),
		firstKeptTurnId: event.firstKeptTurnId,
		compactedTurnCount: event.compactedTurnCount,
		keptTurnCount: event.keptTurnCount,
		tokensBefore: event.tokensBefore,
		keptTurns: event.keptTurns.map(normalizeTurn),
	};
}

function buildHistoryTurns(
	entries: SessionEntry[],
	subagentConversationId: string,
): Turn[] {
	const latestCompactionIndex = entries.findLastIndex(
		(entry) =>
			"subagentConversationId" in entry &&
			entry.subagentConversationId === subagentConversationId &&
			entry.type === "subagent_compaction",
	);
	const latestCompaction =
		latestCompactionIndex >= 0 ? entries[latestCompactionIndex] : undefined;
	const turns: Turn[] = [];
	if (latestCompaction?.type === "subagent_compaction") {
		const summaryTurnId = `${subagentConversationId}:${latestCompaction.id}`;
		turns.push({
			id: summaryTurnId,
			messages: [
				{
					...latestCompaction.message,
					turnId: summaryTurnId,
				},
			],
		});
		turns.push(...latestCompaction.keptTurns.map(normalizeTurn));
	}

	let currentTurn: Turn | null = null;
	const replayEntries =
		latestCompactionIndex >= 0
			? entries.slice(latestCompactionIndex + 1)
			: entries;

	for (const entry of replayEntries) {
		if (
			!("subagentConversationId" in entry) ||
			entry.subagentConversationId !== subagentConversationId
		) {
			continue;
		}

		if (entry.type === "subagent_prompt") {
			const turnId = `${subagentConversationId}:${entry.id}`;
			currentTurn = {
				id: turnId,
				messages: [
					{
						role: "user",
						content: entry.prompt,
						timestamp: new Date(entry.timestamp).getTime(),
						turnId,
					},
				],
			};
			turns.push(currentTurn);
			continue;
		}

		if (entry.type === "subagent_message_completed") {
			if (!currentTurn) {
				const turnId = `${subagentConversationId}:${entry.id}`;
				currentTurn = { id: turnId, messages: [] };
				turns.push(currentTurn);
			}
			currentTurn.messages.push({
				...entry.message,
				turnId: currentTurn.id,
			});
			continue;
		}

		if (entry.type === "subagent_tool_completed") {
			if (!currentTurn) continue;
			const normalized = normalizeToolResultContent(entry.result);
			currentTurn.messages.push({
				role: "toolResult",
				toolCallId: entry.toolCallId,
				toolName: entry.toolName,
				content: normalized.content,
				details: normalized.details,
				isError: entry.isError,
				timestamp: new Date(entry.timestamp).getTime(),
				turnId: currentTurn.id,
			});
		}
	}

	return turns;
}

async function createLiveSubagentRuntime(
	options: SubagentExecutorFactoryOptions,
): Promise<LiveSubagentRuntime> {
	const availableModels = options.runtime.getAvailableModels();
	const resolvedModel = options.definition.model
		? availableModels.find((model) => model.id === options.definition.model)
		: options.runtime.agentInfo.model;
	if (!resolvedModel) {
		throw new SubagentManagerError(
			"RUNTIME_ERROR",
			options.definition.model
				? `Sub-agent "${options.definition.name}" requires unavailable model "${options.definition.model}".`
				: `No active model available for sub-agent "${options.definition.name}".`,
		);
	}

	const now = nowIso();
	const session: Session = {
		id: options.subagentConversationId,
		version: SESSION_VERSION,
		cwd: options.runtime.getSession().cwd,
		name: options.definition.name,
		model: resolvedModel.id,
		thinkingLevel: options.runtime.agentInfo.thinkingLevel,
		createdAt: now,
		updatedAt: now,
		turns: options.historyTurns,
	};
	const runtime = new AgentRuntime(session, {
		settings: options.runtime.settings,
		systemPromptAdditions: [
			...options.runtime.getSystemPromptAdditions(),
			options.definition.instructions,
		],
		extraTools: options.runtime.getTools(),
		subagent: true,
	});

	let writeChain = Promise.resolve();
	let currentMessageId: string | undefined;
	let latestCompletedText: string | undefined;
	let terminalError: string | undefined;
	let aborted = false;
	let abortReason: string | undefined;

	const enqueueEntries = (entries: AppendableSessionEntry[]): void => {
		writeChain = writeChain.then(() => options.onEntries(entries));
	};
	const baseEntry = () => ({
		timestamp: nowIso(),
		agentName: options.definition.name,
		subagentConversationId: options.subagentConversationId,
	});

	const unsubscribe = runtime.subscribe((event) => {
		switch (event.type) {
			case "agent.message.started": {
				currentMessageId = assistantMessageId(event.message) ?? randomUUID();
				options.onTerminalState("running");
				enqueueEntries([
					{
						...baseEntry(),
						type: "subagent_message_started",
						messageId: currentMessageId,
					},
				]);
				break;
			}
			case "agent.tool.ended": {
				enqueueEntries([
					{
						...baseEntry(),
						type: "subagent_tool_completed",
						toolCallId: event.toolCallId,
						toolName: event.toolName,
						result: event.result,
						isError: event.isError,
					},
				]);
				break;
			}
			case "session.message.appended": {
				if (event.message.role !== "assistant") break;
				const messageId =
					currentMessageId ?? assistantMessageId(event.message) ?? randomUUID();
				const persisted = stripTurnId(event.message);
				latestCompletedText = extractAssistantText(persisted);
				options.onCompletedMessage(persisted, latestCompletedText);
				if (event.message.stopReason === "error") {
					terminalError = event.message.errorMessage ?? "Sub-agent failed.";
				} else {
					terminalError = undefined;
				}
				enqueueEntries([
					{
						...baseEntry(),
						type: "subagent_message_completed",
						messageId,
						message: persisted,
					},
				]);
				currentMessageId = undefined;
				break;
			}
			case "session.compaction.completed.auto":
			case "session.compaction.completed.recovery":
			case "session.compaction.completed.adaptation":
			case "session.compaction.completed.manual": {
				enqueueEntries([createSubagentCompactionEntry(event, baseEntry())]);
				break;
			}
			case "agent.run.failed": {
				terminalError = event.error;
				break;
			}
		}
	});

	return {
		async run(prompt: string): Promise<SubagentRunResult> {
			try {
				await runtime.submitUserMessage(prompt);
				await writeChain;
				if (terminalError) {
					options.onTerminalState("failed", { error: terminalError });
					enqueueEntries([
						{
							...baseEntry(),
							type: "subagent_failed",
							error: terminalError,
						},
					]);
					await writeChain;
					return {
						status: "failed",
						message: latestCompletedText,
						error: terminalError,
					};
				}
				options.onTerminalState("idle");
				return {
					status: "completed",
					message: latestCompletedText,
				};
			} catch (error) {
				const errorMessage =
					error instanceof Error ? error.message : String(error);
				if (aborted) {
					options.onTerminalState("aborted", {
						reason: abortReason ?? errorMessage,
					});
					enqueueEntries([
						{
							...baseEntry(),
							type: "subagent_aborted",
							reason: abortReason ?? errorMessage,
						},
					]);
					await writeChain;
					return {
						status: "aborted",
						message: latestCompletedText,
						error: abortReason ?? errorMessage,
					};
				}
				options.onTerminalState("failed", { error: errorMessage });
				enqueueEntries([
					{
						...baseEntry(),
						type: "subagent_failed",
						error: errorMessage,
					},
				]);
				await writeChain;
				return {
					status: "failed",
					message: latestCompletedText,
					error: errorMessage,
				};
			}
		},
		abort(reason?: string): void {
			aborted = true;
			abortReason = trimToUndefined(reason) ?? "Aborted";
			runtime.abort();
		},
		dispose(): void {
			unsubscribe();
			runtime.dispose();
		},
	};
}

export class SubagentManager {
	private readonly conversationsByAgent = new Map<
		string,
		ActiveSubagentConversationState
	>();
	private readonly appendEntries: typeof appendSessionEntries;
	private readonly readEntries: typeof readSessionEntries;
	private readonly createRuntime: (
		options: SubagentExecutorFactoryOptions,
	) => Promise<LiveSubagentRuntime> | LiveSubagentRuntime;
	private readonly subagentStorage: SubagentSessionStorage;
	private readonly usesSeparateStorage: boolean;
	private readonly dismissedConversationIds = new Set<string>();
	private generation = 0;

	constructor(private readonly options: SubagentManagerOptions) {
		this.appendEntries = options.appendEntries ?? appendSessionEntries;
		this.readEntries = options.readEntries ?? readSessionEntries;
		this.subagentStorage = options.subagentStorage ?? {
			appendEntries: appendSubagentSessionEntries,
			create: createSubagentSession,
			delete: deleteSubagentSession,
			readEntries: readSubagentSessionEntries,
			readHeader: readSubagentSessionHeader,
		};
		this.usesSeparateStorage = Boolean(
			options.subagentStorage ||
				(!options.appendEntries && !options.readEntries),
		);
		this.createRuntime = options.createRuntime ?? createLiveSubagentRuntime;
	}

	reset(): void {
		this.generation += 1;
		for (const conversation of this.conversationsByAgent.values()) {
			if (conversation.status === "running") {
				conversation.runtime?.abort("Session closed");
			}
			conversation.runtime?.dispose();
		}
		this.conversationsByAgent.clear();
	}

	async hydrate(
		session: Session = this.options.runtime.getSession(),
	): Promise<void> {
		this.reset();
		const entries = await this.readEntries(session.id);
		for (const entry of entries) {
			if (!("agentName" in entry)) continue;
			switch (entry.type) {
				case "subagent_started":
					this.conversationsByAgent.set(entry.agentName, {
						agentName: entry.agentName,
						subagentConversationId: entry.subagentConversationId,
						status: "idle",
						model: entry.model,
						description: entry.description,
						lastActivityAt: entry.timestamp,
					});
					break;
				case "subagent_prompt":
				case "subagent_tool_started":
				case "subagent_tool_updated":
				case "subagent_tool_completed":
				case "subagent_message_delta":
				case "subagent_thinking_delta":
					this.touch(entry.agentName, entry.subagentConversationId, {
						lastActivityAt: entry.timestamp,
					});
					break;
				case "subagent_message_started":
					this.touch(entry.agentName, entry.subagentConversationId, {
						status: "running",
						lastActivityAt: entry.timestamp,
					});
					break;
				case "subagent_message_completed":
					this.touch(entry.agentName, entry.subagentConversationId, {
						status: "idle",
						lastActivityAt: entry.timestamp,
						latestMessage: extractAssistantText(entry.message),
						failureMessage: undefined,
						abortReason: undefined,
					});
					break;
				case "subagent_thinking_started":
				case "subagent_thinking_completed":
					this.touch(entry.agentName, entry.subagentConversationId, {
						lastActivityAt: entry.timestamp,
					});
					break;
				case "subagent_failed":
					this.touch(entry.agentName, entry.subagentConversationId, {
						status: "failed",
						lastActivityAt: entry.timestamp,
						failureMessage: entry.error,
					});
					break;
				case "subagent_aborted":
					this.touch(entry.agentName, entry.subagentConversationId, {
						status: "aborted",
						lastActivityAt: entry.timestamp,
						abortReason: entry.reason,
					});
					break;
				case "subagent_dismissed": {
					const active = this.conversationsByAgent.get(entry.agentName);
					if (
						active &&
						active.subagentConversationId === entry.subagentConversationId
					) {
						active.runtime?.dispose();
						this.conversationsByAgent.delete(entry.agentName);
					}
					break;
				}
			}
		}

		if (this.usesSeparateStorage) {
			for (const conversation of this.conversationsByAgent.values()) {
				const childEntries = await this.subagentStorage.readEntries(
					conversation.subagentConversationId,
				);
				for (const entry of childEntries) {
					if (entry.type === "subagent_message_started") {
						conversation.status = "running";
						conversation.lastActivityAt = entry.timestamp;
					} else if (entry.type === "subagent_message_completed") {
						conversation.status = "idle";
						conversation.lastActivityAt = entry.timestamp;
						conversation.latestMessage = extractAssistantText(entry.message);
						conversation.failureMessage = undefined;
						conversation.abortReason = undefined;
					} else if (entry.type === "subagent_failed") {
						conversation.status = "failed";
						conversation.lastActivityAt = entry.timestamp;
						conversation.failureMessage = entry.error;
					} else if (entry.type === "subagent_aborted") {
						conversation.status = "aborted";
						conversation.lastActivityAt = entry.timestamp;
						conversation.abortReason = entry.reason;
					}
				}
			}
		}
	}

	getActive(agentName: string): ActiveSubagentConversationState | undefined {
		return this.conversationsByAgent.get(agentName);
	}

	setActive(conversation: ActiveSubagentConversationState): void {
		this.conversationsByAgent.set(conversation.agentName, conversation);
	}

	async dismiss(agentName: string): Promise<boolean> {
		const active = this.conversationsByAgent.get(agentName);
		if (!active) return false;
		const ownerSession = this.options.runtime.getSession();
		this.dismissedConversationIds.add(active.subagentConversationId);
		await active.initializing?.catch(() => {});
		if (active.status === "running") {
			active.runtime?.abort("Dismissed");
			await this.appendEntries(ownerSession, [
				{
					type: "subagent_aborted",
					timestamp: nowIso(),
					agentName,
					subagentConversationId: active.subagentConversationId,
					reason: "Dismissed",
				},
			]);
		}
		await this.appendEntries(ownerSession, [
			{
				type: "subagent_dismissed",
				timestamp: nowIso(),
				agentName,
				subagentConversationId: active.subagentConversationId,
			},
		]);
		active.runtime?.dispose();
		if (this.usesSeparateStorage) {
			await this.subagentStorage.delete(active.subagentConversationId);
		}
		this.conversationsByAgent.delete(agentName);
		return true;
	}

	listActive(): ActiveSubagentConversationState[] {
		return [...this.conversationsByAgent.values()];
	}

	async run(
		agentName: string,
		message: string,
		source: SubagentEventSource = "agent",
	): Promise<SubagentRunResult> {
		const normalizedAgent = agentName.trim();
		const normalizedMessage = message.trim();
		if (!normalizedAgent) {
			throw new SubagentManagerError(
				"INVALID_INPUT",
				"Provide a sub-agent name.",
			);
		}
		if (!normalizedMessage) {
			throw new SubagentManagerError(
				"INVALID_INPUT",
				"Provide a sub-agent message.",
			);
		}

		const definition = this.options
			.getAgents()
			.find((agent) => agent.name === normalizedAgent);
		if (!definition) {
			throw new SubagentManagerError(
				"SUBAGENT_NOT_FOUND",
				`Unknown sub-agent "${normalizedAgent}".`,
			);
		}

		const ownerSession = this.options.runtime.getSession();
		const runGeneration = this.generation;
		let active = this.conversationsByAgent.get(normalizedAgent);
		if (active?.status === "running") {
			throw new SubagentManagerError(
				"SUBAGENT_BUSY",
				`Sub-agent "${normalizedAgent}" is already running.`,
			);
		}

		const isNewConversation = !active;
		if (!active) {
			active = {
				agentName: normalizedAgent,
				subagentConversationId: randomUUID(),
				status: "running",
				model: definition.model,
				description: definition.description,
				lastActivityAt: nowIso(),
			};
			this.conversationsByAgent.set(normalizedAgent, active);
		}

		const conversation = active;
		conversation.status = "running";
		conversation.lastActivityAt = nowIso();
		conversation.failureMessage = undefined;
		conversation.abortReason = undefined;
		let historyTurns: Turn[] = [];
		const initialize = async () => {
			if (isNewConversation) {
				if (this.usesSeparateStorage) {
					await this.subagentStorage.create({
						id: conversation.subagentConversationId,
						ownerSessionId: ownerSession.id,
						cwd: ownerSession.cwd,
						agentName: normalizedAgent,
						description: definition.description,
						model: definition.model,
						thinkingLevel: this.options.runtime.agentInfo.thinkingLevel,
						source,
					});
				}
				await this.appendEntries(ownerSession, [
					{
						type: "subagent_started",
						timestamp: nowIso(),
						agentName: normalizedAgent,
						subagentConversationId: conversation.subagentConversationId,
						source,
						model: definition.model,
						description: definition.description,
					},
				]);
			}

			await this.ensureSeparateSession(
				conversation,
				ownerSession,
				definition,
				source,
			);
			const parentEntries = await this.readEntries(ownerSession.id);
			const historyEntries = this.usesSeparateStorage
				? await this.subagentStorage.readEntries(
						conversation.subagentConversationId,
					)
				: parentEntries;
			historyTurns = buildHistoryTurns(
				historyEntries,
				conversation.subagentConversationId,
			);
			await this.persistSubagentEntries(ownerSession, [
				{
					type: "subagent_prompt",
					timestamp: nowIso(),
					agentName: normalizedAgent,
					subagentConversationId: conversation.subagentConversationId,
					source,
					prompt: normalizedMessage,
				},
			]);
		};
		conversation.initializing = initialize();
		try {
			await conversation.initializing;
		} catch (error) {
			conversation.status = "failed";
			conversation.failureMessage =
				error instanceof Error ? error.message : String(error);
			if (isNewConversation) {
				await this.appendEntries(ownerSession, [
					{
						type: "subagent_dismissed",
						timestamp: nowIso(),
						agentName: normalizedAgent,
						subagentConversationId: conversation.subagentConversationId,
					},
				]).catch(() => {});
				if (this.conversationsByAgent.get(normalizedAgent) === conversation) {
					this.conversationsByAgent.delete(normalizedAgent);
				}
				if (this.usesSeparateStorage) {
					await this.subagentStorage.delete(
						conversation.subagentConversationId,
					);
				}
			}
			throw error;
		} finally {
			conversation.initializing = undefined;
		}
		if (
			this.dismissedConversationIds.has(conversation.subagentConversationId) ||
			this.generation !== runGeneration ||
			this.conversationsByAgent.get(normalizedAgent) !== conversation
		) {
			return { status: "aborted", error: "Session closed" };
		}
		try {
			const createdRuntime = await this.createRuntime({
				runtime: this.options.runtime,
				definition,
				historyTurns,
				subagentConversationId: conversation.subagentConversationId,
				onEntries: (entries) =>
					this.persistSubagentEntries(ownerSession, entries),
				onCompletedMessage: (_message, text) => {
					const current = this.conversationsByAgent.get(normalizedAgent);
					if (current !== conversation || this.generation !== runGeneration)
						return;
					current.latestMessage = text;
					current.lastActivityAt = nowIso();
				},
				onTerminalState: (status, options) => {
					const current = this.conversationsByAgent.get(normalizedAgent);
					if (current !== conversation || this.generation !== runGeneration)
						return;
					current.status = status;
					current.lastActivityAt = nowIso();
					if (status === "failed") current.failureMessage = options?.error;
					if (status === "aborted") current.abortReason = options?.reason;
				},
			});
			if (
				this.dismissedConversationIds.has(
					conversation.subagentConversationId,
				) ||
				this.generation !== runGeneration ||
				this.conversationsByAgent.get(normalizedAgent) !== conversation
			) {
				createdRuntime.abort("Session closed");
				createdRuntime.dispose();
				return { status: "aborted", error: "Session closed" };
			}
			conversation.runtime = createdRuntime;
		} catch (error) {
			conversation.status = "failed";
			conversation.failureMessage =
				error instanceof Error ? error.message : String(error);
			throw error;
		}

		try {
			const runtime = conversation.runtime;
			if (!runtime) {
				throw new SubagentManagerError(
					"RUNTIME_ERROR",
					`Sub-agent "${normalizedAgent}" failed to initialize.`,
				);
			}
			const result = await runtime.run(normalizedMessage);
			runtime.dispose();
			conversation.runtime = undefined;
			return result;
		} catch (error) {
			conversation.runtime?.dispose();
			conversation.runtime = undefined;
			throw error;
		}
	}

	private touch(
		agentName: string,
		subagentConversationId: string,
		changes: Partial<ActiveSubagentConversationState>,
	): void {
		const active = this.conversationsByAgent.get(agentName);
		if (!active || active.subagentConversationId !== subagentConversationId) {
			return;
		}
		Object.assign(active, changes);
	}

	private async ensureSeparateSession(
		active: ActiveSubagentConversationState,
		ownerSession: Session,
		definition: SubagentDefinition,
		source: SubagentEventSource,
	): Promise<void> {
		if (!this.usesSeparateStorage) return;
		const existing = await this.subagentStorage.readHeader(
			active.subagentConversationId,
		);
		if (existing) return;
		await this.subagentStorage.create({
			id: active.subagentConversationId,
			ownerSessionId: ownerSession.id,
			cwd: ownerSession.cwd,
			agentName: active.agentName,
			description: definition.description,
			model: definition.model,
			thinkingLevel: this.options.runtime.agentInfo.thinkingLevel,
			source,
		});
	}

	private async persistSubagentEntries(
		ownerSession: Session,
		entries: AppendableSessionEntry[],
	): Promise<void> {
		if (!this.usesSeparateStorage) {
			await this.appendEntries(ownerSession, entries);
			return;
		}
		const conversationId = entries.find(
			(entry) => "subagentConversationId" in entry,
		)?.subagentConversationId;
		if (!conversationId || this.dismissedConversationIds.has(conversationId)) {
			return;
		}
		const childEntries = entries.filter((entry) => this.isChildEntry(entry));
		if (childEntries.length > 0) {
			await this.subagentStorage.appendEntries(conversationId, childEntries);
		}
		const parentEntries = entries.filter((entry) =>
			this.isParentReferenceEntry(entry),
		);
		if (parentEntries.length > 0) {
			await this.appendEntries(ownerSession, parentEntries);
		}
	}

	private isChildEntry(entry: AppendableSessionEntry): boolean {
		return (
			entry.type === "subagent_prompt" ||
			entry.type === "subagent_message_started" ||
			entry.type === "subagent_message_completed" ||
			entry.type === "subagent_tool_completed" ||
			entry.type === "subagent_compaction" ||
			entry.type === "subagent_failed" ||
			entry.type === "subagent_aborted"
		);
	}

	private isParentReferenceEntry(entry: AppendableSessionEntry): boolean {
		return (
			entry.type === "subagent_prompt" ||
			entry.type === "subagent_failed" ||
			entry.type === "subagent_aborted"
		);
	}
}

export function isSubagentFailureEntry(
	entry: SessionEntry,
): entry is SubagentFailedEntry {
	return entry.type === "subagent_failed";
}

export function isSubagentAbortedEntry(
	entry: SessionEntry,
): entry is SubagentAbortedEntry {
	return entry.type === "subagent_aborted";
}
