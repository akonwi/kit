import { randomUUID } from "node:crypto";
import type { AgentTool } from "@mariozechner/pi-agent-core";
import type { ImageContent, TextContent } from "@mariozechner/pi-ai";
import { getApiKey } from "../../auth";
import { buildSystemPrompt } from "../../context/agents";
import {
	type AgentRuntime,
	DEFAULT_SYSTEM_PROMPT,
} from "../../runtime/agent-runtime";
import { type AgentEvent, KitAgent } from "../../runtime/kit-agent";
import {
	type AppendableSessionEntry,
	appendSessionEntries,
	type PersistedKitAgentMessage,
	readSessionEntries,
	type Session,
	type SessionEntry,
	type SubagentAbortedEntry,
	type SubagentEventSource,
	type SubagentFailedEntry,
	type Turn,
} from "../../session";
import { resolveRetrySettings } from "../../settings";
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

interface SubagentManagerOptions {
	runtime: RuntimeLike;
	getAgents: () => SubagentDefinition[];
	appendEntries?: typeof appendSessionEntries;
	readEntries?: typeof readSessionEntries;
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

function maybeTextDelta(
	event: Extract<AgentEvent, { type: "message_update" }>,
): string | null {
	if (event.message.role !== "assistant") return null;
	const deltaEvent = event.assistantMessageEvent as
		| { type?: string; delta?: unknown }
		| undefined;
	if (!deltaEvent || typeof deltaEvent.delta !== "string") return null;
	if (deltaEvent.type === "thinking_delta") return null;
	return deltaEvent.delta;
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

function buildHistoryTurns(
	entries: SessionEntry[],
	subagentConversationId: string,
): Turn[] {
	const turns: Turn[] = [];
	let currentTurn: Turn | null = null;

	for (const entry of entries) {
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

	const tools = options.runtime
		.getTools()
		.filter((tool: AgentTool) => tool.name !== "subagent");
	const basePrompt = [
		DEFAULT_SYSTEM_PROMPT,
		...options.runtime.getSystemPromptAdditions(),
		options.definition.instructions,
	]
		.filter((part) => part.trim().length > 0)
		.join("\n\n");
	const systemPrompt = buildSystemPrompt(
		basePrompt,
		options.runtime.getContextFiles(),
	);

	const agent = new KitAgent({
		initialTurns: options.historyTurns,
		initialState: {
			model: resolvedModel,
			thinkingLevel: options.runtime.agentInfo.thinkingLevel,
			systemPrompt,
			tools,
		},
		getApiKey: (provider) => getApiKey(provider),
		maxRetryDelayMs: resolveRetrySettings(options.runtime.settings.retry)
			.maxDelayMs,
	});
	agent.sessionId = options.runtime.getSession().id;

	let writeChain = Promise.resolve();
	let currentMessageId: string | undefined;
	let latestCompletedText: string | undefined;
	let aborted = false;
	let abortReason: string | undefined;

	const enqueueEntries = (entries: AppendableSessionEntry[]): void => {
		writeChain = writeChain.then(() => options.onEntries(entries));
	};

	const unsubscribe = agent.subscribe((event) => {
		switch (event.type) {
			case "assistant_message_started": {
				currentMessageId = assistantMessageId(event.message) ?? randomUUID();
				options.onTerminalState("running");
				enqueueEntries([
					{
						type: "subagent_message_started",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
						messageId: currentMessageId,
					},
					{
						type: "subagent_thinking_started",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
						messageId: currentMessageId,
					},
				]);
				break;
			}
			case "message_update": {
				const delta = maybeTextDelta(event);
				if (!delta || !currentMessageId) break;
				enqueueEntries([
					{
						type: "subagent_message_delta",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
						messageId: currentMessageId,
						delta,
					},
				]);
				break;
			}
			case "agent_thinking_updated": {
				if (!currentMessageId) break;
				enqueueEntries([
					{
						type: "subagent_thinking_delta",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
						messageId: currentMessageId,
						delta: event.delta,
					},
				]);
				break;
			}
			case "agent_tool_started": {
				enqueueEntries([
					{
						type: "subagent_tool_started",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
						toolCallId: event.toolCallId,
						toolName: event.toolName,
						args: event.args,
					},
				]);
				break;
			}
			case "agent_tool_updated": {
				enqueueEntries([
					{
						type: "subagent_tool_updated",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
						toolCallId: event.toolCallId,
						toolName: event.toolName,
						partialResult: event.partialResult,
					},
				]);
				break;
			}
			case "agent_tool_ended": {
				enqueueEntries([
					{
						type: "subagent_tool_completed",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
						toolCallId: event.toolCallId,
						toolName: event.toolName,
						result: event.result,
						isError: event.isError,
					},
				]);
				break;
			}
			case "assistant_message_ended": {
				const messageId =
					currentMessageId ?? assistantMessageId(event.message) ?? randomUUID();
				const persisted = stripTurnId(event.message);
				latestCompletedText = extractAssistantText(persisted);
				options.onCompletedMessage(persisted, latestCompletedText);
				enqueueEntries([
					{
						type: "subagent_thinking_completed",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
						messageId,
					},
					{
						type: "subagent_message_completed",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
						messageId,
						message: persisted,
					},
				]);
				currentMessageId = undefined;
				break;
			}
		}
	});

	return {
		async run(prompt: string): Promise<SubagentRunResult> {
			try {
				await agent.prompt(prompt);
				await writeChain;
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
							type: "subagent_aborted",
							timestamp: nowIso(),
							agentName: options.definition.name,
							subagentConversationId: options.subagentConversationId,
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
						type: "subagent_failed",
						timestamp: nowIso(),
						agentName: options.definition.name,
						subagentConversationId: options.subagentConversationId,
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
			agent.abort();
		},
		dispose(): void {
			unsubscribe();
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

	constructor(private readonly options: SubagentManagerOptions) {
		this.appendEntries = options.appendEntries ?? appendSessionEntries;
		this.readEntries = options.readEntries ?? readSessionEntries;
		this.createRuntime = options.createRuntime ?? createLiveSubagentRuntime;
	}

	reset(): void {
		for (const conversation of this.conversationsByAgent.values()) {
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
		if (active.status === "running") {
			active.runtime?.abort("Dismissed");
			await this.appendSubagentEntries([
				{
					type: "subagent_aborted",
					timestamp: nowIso(),
					agentName,
					subagentConversationId: active.subagentConversationId,
					reason: "Dismissed",
				},
			]);
		}
		await this.appendSubagentEntries([
			{
				type: "subagent_dismissed",
				timestamp: nowIso(),
				agentName,
				subagentConversationId: active.subagentConversationId,
			},
		]);
		active.runtime?.dispose();
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

		let active = this.conversationsByAgent.get(normalizedAgent);
		if (active?.status === "running") {
			throw new SubagentManagerError(
				"SUBAGENT_BUSY",
				`Sub-agent "${normalizedAgent}" is already running.`,
			);
		}

		if (!active) {
			active = {
				agentName: normalizedAgent,
				subagentConversationId: randomUUID(),
				status: "idle",
				model: definition.model,
				description: definition.description,
				lastActivityAt: nowIso(),
			};
			this.conversationsByAgent.set(normalizedAgent, active);
			await this.appendSubagentEntries([
				{
					type: "subagent_started",
					timestamp: nowIso(),
					agentName: normalizedAgent,
					subagentConversationId: active.subagentConversationId,
					source,
					model: definition.model,
					description: definition.description,
				},
			]);
		}

		const historyEntries = await this.readEntries(
			this.options.runtime.getSession().id,
		);
		const historyTurns = buildHistoryTurns(
			historyEntries,
			active.subagentConversationId,
		);

		active.lastActivityAt = nowIso();
		active.failureMessage = undefined;
		active.abortReason = undefined;
		await this.appendSubagentEntries([
			{
				type: "subagent_prompt",
				timestamp: nowIso(),
				agentName: normalizedAgent,
				subagentConversationId: active.subagentConversationId,
				source,
				prompt: normalizedMessage,
			},
		]);
		const conversation = active;
		conversation.status = "running";
		conversation.lastActivityAt = nowIso();
		try {
			conversation.runtime = await this.createRuntime({
				runtime: this.options.runtime,
				definition,
				historyTurns,
				subagentConversationId: conversation.subagentConversationId,
				onEntries: (entries) => this.appendSubagentEntries(entries),
				onCompletedMessage: (_message, text) => {
					const current = this.conversationsByAgent.get(normalizedAgent);
					if (!current) return;
					current.latestMessage = text;
					current.lastActivityAt = nowIso();
				},
				onTerminalState: (status, options) => {
					const current = this.conversationsByAgent.get(normalizedAgent);
					if (!current) return;
					current.status = status;
					current.lastActivityAt = nowIso();
					if (status === "failed") current.failureMessage = options?.error;
					if (status === "aborted") current.abortReason = options?.reason;
				},
			});
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

	private async appendSubagentEntries(
		entries: AppendableSessionEntry[],
	): Promise<void> {
		await this.appendEntries(this.options.runtime.getSession(), entries);
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
