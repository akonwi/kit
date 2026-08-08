export type ConnectionPhase =
	| "disconnected"
	| "connecting"
	| "synchronizing"
	| "live";

export type ToolActivity = {
	id: string;
	name: string;
	args?: unknown;
	result?: unknown;
	isError: boolean;
	status: "running" | "complete";
};

export type ClientState = {
	phase: ConnectionPhase;
	streamId: string | null;
	sequence: number;
	serverState: Record<string, unknown>;
	messages: unknown[];
	messageKeys: string[];
	messageOffset: number;
	totalMessageCount: number;
	activeMessageIndex: number | null;
	pendingActiveMessageDeltas: Array<Record<string, unknown>>;
	tools: ToolActivity[];
	pendingInteractions: unknown[];
	pendingInteractionOffset: number;
	totalPendingInteractionCount: number;
	interactionRevision: number;
	queuedMessageCount: number;
	lastError: string | null;
};

export class ProtocolSyncError extends Error {}

export function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function createClientState(): ClientState {
	return {
		phase: "disconnected",
		streamId: null,
		sequence: 0,
		serverState: {},
		messages: [],
		messageKeys: [],
		messageOffset: 0,
		totalMessageCount: 0,
		activeMessageIndex: null,
		pendingActiveMessageDeltas: [],
		tools: [],
		pendingInteractions: [],
		pendingInteractionOffset: 0,
		totalPendingInteractionCount: 0,
		interactionRevision: 0,
		queuedMessageCount: 0,
		lastError: null,
	};
}

export function withConnectionPhase(
	state: ClientState,
	phase: ConnectionPhase,
): ClientState {
	return { ...state, phase };
}

export function hydrateMessageReference(
	state: ClientState,
	index: number,
	reference: unknown,
	hydrated: unknown,
	replayPendingDeltas = true,
): ClientState {
	if (state.messages[index] !== reference) return state;
	let message = hydrated;
	if (index === state.activeMessageIndex && replayPendingDeltas) {
		for (const delta of state.pendingActiveMessageDeltas) {
			message = cloneMessageWithDelta(message, delta);
		}
	}
	const messages = [...state.messages];
	messages[index] = message;
	return {
		...state,
		messages,
		pendingActiveMessageDeltas:
			index === state.activeMessageIndex
				? []
				: state.pendingActiveMessageDeltas,
	};
}

export function prependMessages(
	state: ClientState,
	messages: unknown[],
	offset: number,
	totalMessageCount: number,
): ClientState {
	if (!Number.isSafeInteger(offset) || offset < 0) return state;
	return {
		...state,
		messages: [...messages, ...state.messages],
		messageKeys: [
			...messages.map((_message, index) => `message:${offset + index}`),
			...state.messageKeys,
		],
		messageOffset: offset,
		totalMessageCount,
		activeMessageIndex:
			state.activeMessageIndex === null
				? null
				: state.activeMessageIndex + messages.length,
	};
}

function cloneMessageWithDelta(
	message: unknown,
	event: Record<string, unknown>,
): unknown {
	if (!isRecord(message) || !Array.isArray(message.content)) return message;
	const contentIndex = event.contentIndex;
	if (
		typeof contentIndex !== "number" ||
		!Number.isSafeInteger(contentIndex) ||
		contentIndex < 0
	) {
		return message;
	}
	const content = [...message.content];
	const eventType = event.type;
	const delta = typeof event.delta === "string" ? event.delta : "";
	if (eventType === "text_start") {
		content[contentIndex] = { type: "text", text: "" };
	} else if (eventType === "thinking_start") {
		content[contentIndex] = { type: "thinking", thinking: "" };
	} else if (eventType === "text_delta") {
		const existing = content[contentIndex];
		const text =
			isRecord(existing) && typeof existing.text === "string"
				? existing.text
				: "";
		content[contentIndex] = { type: "text", text: `${text}${delta}` };
	} else if (eventType === "thinking_delta") {
		const existing = content[contentIndex];
		const thinking =
			isRecord(existing) && typeof existing.thinking === "string"
				? existing.thinking
				: "";
		content[contentIndex] = {
			type: "thinking",
			thinking: `${thinking}${delta}`,
		};
	}
	return { ...message, content };
}

function queuedCount(record: Record<string, unknown>): number {
	const steering = Array.isArray(record.steering) ? record.steering.length : 0;
	const followUp = Array.isArray(record.followUp) ? record.followUp.length : 0;
	return steering + followUp;
}

function applyEvent(
	state: ClientState,
	record: Record<string, unknown>,
): ClientState {
	switch (record.type) {
		case "state_changed": {
			if (!isRecord(record.state)) return state;
			const currentSessionId = state.serverState.sessionId;
			const nextSessionId = record.state.sessionId;
			if (
				typeof currentSessionId === "string" &&
				typeof nextSessionId === "string" &&
				currentSessionId !== nextSessionId
			) {
				throw new ProtocolSyncError("The active session changed");
			}
			return { ...state, serverState: record.state };
		}
		case "agent_start":
			return {
				...state,
				serverState: { ...state.serverState, isStreaming: true },
				lastError: null,
			};
		case "turn_start":
			return { ...state, tools: [] };
		case "agent_settled":
			return {
				...state,
				serverState: { ...state.serverState, isStreaming: false },
				activeMessageIndex: null,
				pendingActiveMessageDeltas: [],
			};
		case "message_start": {
			const messages = [...state.messages, record.message];
			return {
				...state,
				messages,
				messageKeys: [
					...state.messageKeys,
					`message:${state.messageOffset + messages.length - 1}`,
				],
				totalMessageCount: Math.max(
					state.totalMessageCount + 1,
					state.messageOffset + messages.length,
				),
				activeMessageIndex: messages.length - 1,
				pendingActiveMessageDeltas: [],
			};
		}
		case "message_update": {
			if (
				state.activeMessageIndex === null ||
				!isRecord(record.assistantMessageEvent)
			) {
				return state;
			}
			const activeMessage = state.messages[state.activeMessageIndex];
			if (
				isRecord(activeMessage) &&
				activeMessage.type === "message_reference"
			) {
				return {
					...state,
					pendingActiveMessageDeltas: [
						...state.pendingActiveMessageDeltas,
						record.assistantMessageEvent,
					],
				};
			}
			const messages = [...state.messages];
			messages[state.activeMessageIndex] = cloneMessageWithDelta(
				activeMessage,
				record.assistantMessageEvent,
			);
			return { ...state, messages };
		}
		case "message_end": {
			if (state.activeMessageIndex === null) return state;
			const messages = [...state.messages];
			messages[state.activeMessageIndex] = record.message;
			return {
				...state,
				messages,
				activeMessageIndex: null,
				pendingActiveMessageDeltas: [],
			};
		}
		case "tool_execution_start": {
			if (typeof record.toolCallId !== "string") return state;
			return {
				...state,
				tools: [
					...state.tools.filter((tool) => tool.id !== record.toolCallId),
					{
						id: record.toolCallId,
						name:
							typeof record.toolName === "string" ? record.toolName : "tool",
						args: record.args,
						isError: false,
						status: "running",
					},
				],
			};
		}
		case "tool_execution_update":
		case "tool_execution_end": {
			if (typeof record.toolCallId !== "string") return state;
			return {
				...state,
				tools: state.tools.map((tool) =>
					tool.id === record.toolCallId
						? {
								...tool,
								result:
									record.type === "tool_execution_end"
										? record.result
										: record.partialResult,
								isError: record.isError === true,
								status:
									record.type === "tool_execution_end" ? "complete" : "running",
							}
						: tool,
				),
			};
		}
		case "queue_update":
			return { ...state, queuedMessageCount: queuedCount(record) };
		case "ui_request": {
			const incomingRequest = record.request;
			if (
				!isRecord(incomingRequest) ||
				typeof incomingRequest.id !== "string"
			) {
				return state;
			}
			return {
				...state,
				pendingInteractions: [
					...state.pendingInteractions.filter(
						(request) =>
							!isRecord(request) || request.id !== incomingRequest.id,
					),
					incomingRequest,
				],
				interactionRevision: state.interactionRevision + 1,
				totalPendingInteractionCount: state.pendingInteractions.some(
					(request) => isRecord(request) && request.id === incomingRequest.id,
				)
					? state.totalPendingInteractionCount
					: state.totalPendingInteractionCount + 1,
			};
		}
		case "ui_resolved": {
			const requestId = record.requestId;
			return typeof requestId === "string"
				? {
						...state,
						pendingInteractions: state.pendingInteractions.filter(
							(request) => !isRecord(request) || request.id !== requestId,
						),
						interactionRevision: state.interactionRevision + 1,
						totalPendingInteractionCount: Math.max(
							0,
							state.totalPendingInteractionCount - 1,
						),
					}
				: state;
		}
		case "error":
			return {
				...state,
				lastError:
					typeof record.error === "string"
						? record.error
						: "The session reported an error.",
			};
		default:
			return state;
	}
}

export function reduceClientRecord(
	state: ClientState,
	record: unknown,
): ClientState {
	if (!isRecord(record) || typeof record.type !== "string") return state;
	if (record.type === "sync") {
		if (
			typeof record.streamId !== "string" ||
			typeof record.sequence !== "number"
		) {
			throw new ProtocolSyncError("Invalid synchronization record");
		}
		if (record.mode === "snapshot") {
			const serverState = isRecord(record.state) ? record.state : {};
			const messages = Array.isArray(record.messages) ? record.messages : [];
			const finalMessage = messages.at(-1);
			const activeMessageIndex =
				serverState.isStreaming === true &&
				isRecord(finalMessage) &&
				finalMessage.role === "assistant"
					? messages.length - 1
					: null;
			return {
				...state,
				phase: "synchronizing",
				streamId: record.streamId,
				sequence: record.sequence,
				serverState,
				messages,
				messageKeys: messages.map(
					(_message, index) =>
						`message:${(typeof record.messageOffset === "number" ? record.messageOffset : 0) + index}`,
				),
				messageOffset:
					typeof record.messageOffset === "number" ? record.messageOffset : 0,
				totalMessageCount:
					typeof record.totalMessageCount === "number"
						? record.totalMessageCount
						: messages.length,
				activeMessageIndex,
				pendingActiveMessageDeltas: [],
				tools: [],
				pendingInteractions: Array.isArray(record.pendingInteractions)
					? record.pendingInteractions
					: [],
				pendingInteractionOffset:
					typeof record.pendingInteractionOffset === "number"
						? record.pendingInteractionOffset
						: 0,
				totalPendingInteractionCount:
					typeof record.totalPendingInteractionCount === "number"
						? record.totalPendingInteractionCount
						: Array.isArray(record.pendingInteractions)
							? record.pendingInteractions.length
							: 0,
				interactionRevision: state.interactionRevision + 1,
				queuedMessageCount:
					typeof serverState.pendingMessageCount === "number"
						? serverState.pendingMessageCount
						: 0,
				lastError: null,
			};
		}
		if (
			record.mode !== "replay" ||
			state.streamId !== record.streamId ||
			state.sequence !== record.sequence
		) {
			throw new ProtocolSyncError("Replay does not match client state");
		}
		return { ...state, phase: "synchronizing", lastError: null };
	}

	if (record.type === "sync_complete") {
		if (
			record.streamId !== state.streamId ||
			record.sequence !== state.sequence
		) {
			throw new ProtocolSyncError("Synchronization ended at the wrong cursor");
		}
		return { ...state, phase: "live" };
	}

	if (typeof record.sequence === "number") {
		if (record.streamId !== state.streamId) {
			throw new ProtocolSyncError("Event belongs to another stream");
		}
		if (record.sequence <= state.sequence) return state;
		if (record.sequence !== state.sequence + 1) {
			throw new ProtocolSyncError("Event sequence gap");
		}
		const next = applyEvent(state, record);
		return { ...next, sequence: record.sequence };
	}
	return applyEvent(state, record);
}
