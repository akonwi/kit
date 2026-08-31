import {
	EMPTY_REMOTE_CHROME,
	parseRemoteChromeSnapshot,
	type RemoteChromeSnapshot,
} from "./chrome-state";
import {
	MAX_REMOTE_MESSAGE_PREVIEW_LENGTH,
	MAX_REMOTE_MESSAGE_PREVIEWS,
	remoteMessagePreviews,
} from "./message-previews";

export type ConnectionPhase =
	| "disconnected"
	| "connecting"
	| "synchronizing"
	| "live";

export type ToolActivity = {
	id: string;
	turnId: string;
	name: string;
	args?: Record<string, unknown>;
	partialResult?: unknown;
	result?: unknown;
	isError: boolean;
	status: "running" | "complete";
};

export type ClientState = {
	phase: ConnectionPhase;
	streamId: string | null;
	sequence: number;
	serverState: Record<string, unknown>;
	chrome: RemoteChromeSnapshot;
	messages: unknown[];
	messageKeys: string[];
	messageOffset: number;
	totalMessageCount: number;
	activeMessageIndex: number | null;
	activeTurnId: string | null;
	pendingActiveMessageDeltas: Array<Record<string, unknown>>;
	tools: ToolActivity[];
	pendingStatus: string | null;
	pendingInteractions: unknown[];
	pendingInteractionOffset: number;
	totalPendingInteractionCount: number;
	pendingInteractionGeneration: number;
	queuedMessageCount: number;
	queuedMessageGeneration: number;
	queuedMessagePreviews: string[];
	lastError: string | null;
};

export class ProtocolSyncError extends Error {}

export class ProtocolRebaseRequired extends ProtocolSyncError {}

export function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function createClientState(): ClientState {
	return {
		phase: "disconnected",
		streamId: null,
		sequence: 0,
		serverState: {},
		chrome: EMPTY_REMOTE_CHROME,
		messages: [],
		messageKeys: [],
		messageOffset: 0,
		totalMessageCount: 0,
		activeMessageIndex: null,
		activeTurnId: null,
		pendingActiveMessageDeltas: [],
		tools: [],
		pendingStatus: null,
		pendingInteractions: [],
		pendingInteractionOffset: 0,
		totalPendingInteractionCount: 0,
		pendingInteractionGeneration: 0,
		queuedMessageCount: 0,
		queuedMessageGeneration: 0,
		queuedMessagePreviews: [],
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

function messageKey(message: unknown, fallback: string): string {
	return isRecord(message) && typeof message.messageId === "string"
		? message.messageId
		: fallback;
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
			...messages.map((message, index) =>
				messageKey(message, `message:${offset + index}`),
			),
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
	const kind = event.kind;
	const contentType = event.contentType;
	const delta = typeof event.delta === "string" ? event.delta : "";
	if (kind === "content.started" && contentType === "text") {
		content[contentIndex] = { type: "text", text: "" };
	} else if (kind === "content.started" && contentType === "thinking") {
		content[contentIndex] = { type: "thinking", thinking: "" };
	} else if (kind === "content.delta" && contentType === "text") {
		const existing = content[contentIndex];
		const text =
			isRecord(existing) && typeof existing.text === "string"
				? existing.text
				: "";
		content[contentIndex] = { type: "text", text: `${text}${delta}` };
	} else if (kind === "content.delta" && contentType === "thinking") {
		const existing = content[contentIndex];
		const thinking =
			isRecord(existing) && typeof existing.thinking === "string"
				? existing.thinking
				: "";
		content[contentIndex] = {
			type: "thinking",
			thinking: `${thinking}${delta}`,
		};
	} else if (
		kind === "content.completed" &&
		typeof event.content === "string" &&
		(contentType === "text" || contentType === "thinking")
	) {
		content[contentIndex] =
			contentType === "text"
				? { type: "text", text: event.content }
				: { type: "thinking", thinking: event.content };
	}
	return { ...message, content };
}

function messageQueueState(
	record: Record<string, unknown>,
	source: "event" | "snapshot",
): {
	count: number;
	generation: number | null;
	previews: string[];
} | null {
	const count = record.count;
	const generation = record.generation;
	const previews = record.previews;
	if (
		generation !== undefined &&
		(typeof generation !== "number" ||
			!Number.isSafeInteger(generation) ||
			generation < 0)
	) {
		return null;
	}
	if (count === undefined && previews === undefined) {
		if (
			Array.isArray(record.followUp) &&
			record.followUp.every((message) => typeof message === "string")
		) {
			return {
				count: record.followUp.length,
				generation: typeof generation === "number" ? generation : null,
				previews: remoteMessagePreviews(record.followUp),
			};
		}
		return source === "snapshot"
			? { count: 0, generation: 0, previews: [] }
			: null;
	}
	if (
		source === "snapshot" &&
		typeof count === "number" &&
		Number.isSafeInteger(count) &&
		count >= 0 &&
		previews === undefined
	) {
		return {
			count,
			generation: typeof generation === "number" ? generation : 0,
			previews: [],
		};
	}
	if (
		typeof count !== "number" ||
		!Number.isSafeInteger(count) ||
		count < 0 ||
		!Array.isArray(previews) ||
		previews.length > MAX_REMOTE_MESSAGE_PREVIEWS ||
		previews.length > count ||
		!previews.every(
			(preview) =>
				typeof preview === "string" &&
				Array.from(preview).length <= MAX_REMOTE_MESSAGE_PREVIEW_LENGTH,
		)
	) {
		return null;
	}
	return {
		count,
		generation:
			typeof generation === "number"
				? generation
				: source === "snapshot"
					? 0
					: null,
		previews,
	};
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
				throw new ProtocolRebaseRequired("The active session changed");
			}
			return {
				...state,
				serverState: record.state,
				activeTurnId:
					record.state.isStreaming === false ? null : state.activeTurnId,
			};
		}
		case "scratchpad.changed":
			if (
				typeof record.sessionId !== "string" ||
				typeof record.content !== "string"
			) {
				return state;
			}
			return {
				...state,
				serverState: {
					...state.serverState,
					scratchpad: {
						sessionId: record.sessionId,
						content: record.content,
					},
				},
			};
		case "agent.start":
			return {
				...state,
				serverState: { ...state.serverState, isStreaming: true },
				pendingStatus: "Working…",
				lastError: null,
			};
		case "agent.turn.started":
			return {
				...state,
				activeTurnId:
					typeof record.turnId === "string"
						? record.turnId
						: state.activeTurnId,
				tools: [],
				pendingStatus: "Working…",
			};
		case "agent.turn.completed":
			return {
				...state,
				activeTurnId:
					typeof record.turnId !== "string" ||
					record.turnId === state.activeTurnId
						? null
						: state.activeTurnId,
			};
		case "agent.settled":
			return {
				...state,
				serverState: { ...state.serverState, isStreaming: false },
				activeMessageIndex: null,
				activeTurnId: null,
				pendingActiveMessageDeltas: [],
				pendingStatus: null,
			};
		case "user.message.created":
		case "agent.message.started":
		case "session.message.appended":
		case "session.handoff_summary.appended": {
			if (!isRecord(record.message)) return state;
			const id =
				typeof record.messageId === "string"
					? record.messageId
					: typeof record.message.messageId === "string"
						? record.message.messageId
						: null;
			const existingIndex = id === null ? -1 : state.messageKeys.indexOf(id);
			const messages = [...state.messages];
			const messageKeys = [...state.messageKeys];
			let messageIndex = existingIndex;
			if (existingIndex >= 0) {
				messages[existingIndex] = record.message;
			} else {
				messageIndex = messages.length;
				messages.push(record.message);
				messageKeys.push(id ?? `message:${state.messageOffset + messageIndex}`);
			}
			const activates = record.type === "agent.message.started";
			return {
				...state,
				messages,
				messageKeys,
				totalMessageCount:
					existingIndex >= 0
						? state.totalMessageCount
						: Math.max(
								state.totalMessageCount + 1,
								state.messageOffset + messages.length,
							),
				activeMessageIndex: activates ? messageIndex : state.activeMessageIndex,
				pendingActiveMessageDeltas: activates
					? []
					: state.pendingActiveMessageDeltas,
			};
		}
		case "agent.message.updated": {
			if (typeof record.messageId !== "string" || !isRecord(record.update)) {
				return state;
			}
			const messageIndex = state.messageKeys.indexOf(record.messageId);
			if (messageIndex < 0) return state;
			const activeMessage = state.messages[messageIndex];
			if (
				isRecord(activeMessage) &&
				activeMessage.type === "message_reference"
			) {
				return {
					...state,
					activeMessageIndex: messageIndex,
					pendingActiveMessageDeltas: [
						...state.pendingActiveMessageDeltas,
						record.update,
					],
				};
			}
			const messages = [...state.messages];
			messages[messageIndex] = cloneMessageWithDelta(
				activeMessage,
				record.update,
			);
			return { ...state, messages, activeMessageIndex: messageIndex };
		}
		case "agent.message.ended": {
			if (typeof record.messageId !== "string" || !isRecord(record.message)) {
				return state;
			}
			const messageIndex = state.messageKeys.indexOf(record.messageId);
			if (messageIndex < 0) return state;
			const messages = [...state.messages];
			messages[messageIndex] = record.message;
			return {
				...state,
				messages,
				activeMessageIndex: null,
				pendingActiveMessageDeltas: [],
			};
		}
		case "agent.tool.started": {
			if (
				typeof record.toolCallId !== "string" ||
				typeof record.turnId !== "string"
			) {
				return state;
			}
			return {
				...state,
				tools: [
					...state.tools.filter((tool) => tool.id !== record.toolCallId),
					{
						id: record.toolCallId,
						turnId: record.turnId,
						name:
							typeof record.toolName === "string" ? record.toolName : "tool",
						args: isRecord(record.args) ? record.args : undefined,
						isError: false,
						status: "running",
					},
				],
			};
		}
		case "agent.tool.updated":
		case "agent.tool.ended": {
			if (
				typeof record.toolCallId !== "string" ||
				typeof record.turnId !== "string"
			) {
				return state;
			}
			const existing = state.tools.find(
				(tool) => tool.id === record.toolCallId,
			);
			const updated: ToolActivity = {
				id: record.toolCallId,
				turnId: record.turnId,
				name:
					typeof record.toolName === "string"
						? record.toolName
						: (existing?.name ?? "tool"),
				args: isRecord(record.args) ? record.args : existing?.args,
				partialResult:
					record.type === "agent.tool.updated"
						? record.partialResult
						: existing?.partialResult,
				result:
					record.type === "agent.tool.ended" ? record.result : existing?.result,
				isError:
					record.type === "agent.tool.ended"
						? record.isError === true
						: (existing?.isError ?? false),
				status: record.type === "agent.tool.ended" ? "complete" : "running",
			};
			return {
				...state,
				tools: existing
					? state.tools.map((tool) =>
							tool.id === record.toolCallId ? updated : tool,
						)
					: [...state.tools, updated],
			};
		}
		case "agent.retry.started": {
			const attempt = typeof record.attempt === "number" ? record.attempt : "?";
			const maxAttempts =
				typeof record.maxAttempts === "number" ? record.maxAttempts : "?";
			const delaySeconds =
				typeof record.delayMs === "number"
					? Math.ceil(record.delayMs / 1000)
					: 0;
			return {
				...state,
				pendingStatus: `Retrying (${attempt}/${maxAttempts}) in ${delaySeconds}s…`,
			};
		}
		case "agent.retry.completed":
			return { ...state, pendingStatus: "Working…" };
		case "agent.retry.failed":
			return { ...state, activeTurnId: null, pendingStatus: null };
		case "chat.message-queue.changed": {
			const queue = messageQueueState(record, "event");
			if (!queue) {
				throw new ProtocolSyncError("Invalid queued message state");
			}
			return {
				...state,
				queuedMessageCount: queue.count,
				queuedMessageGeneration:
					queue.generation ?? state.queuedMessageGeneration,
				queuedMessagePreviews: queue.previews,
			};
		}
		case "shell.chrome.changed": {
			const chrome = parseRemoteChromeSnapshot(record.chrome);
			if (!chrome)
				throw new ProtocolSyncError("Invalid chrome contribution state");
			return { ...state, chrome };
		}
		case "ui_snapshot": {
			if (
				typeof record.generation !== "number" ||
				!Number.isSafeInteger(record.generation) ||
				record.generation < 0 ||
				!Array.isArray(record.requests) ||
				!record.requests.every(
					(request) => isRecord(request) && typeof request.id === "string",
				)
			) {
				throw new ProtocolSyncError("Invalid interaction snapshot");
			}
			return {
				...state,
				pendingInteractions: record.requests,
				pendingInteractionOffset: 0,
				totalPendingInteractionCount: record.requests.length,
				pendingInteractionGeneration: record.generation,
			};
		}
		case "ui_request": {
			if (
				typeof record.generation !== "number" ||
				!Number.isSafeInteger(record.generation) ||
				record.generation !== state.pendingInteractionGeneration + 1
			) {
				throw new ProtocolSyncError("Interaction generation mismatch");
			}
			const incomingRequest = record.request;
			if (
				!isRecord(incomingRequest) ||
				typeof incomingRequest.id !== "string"
			) {
				throw new ProtocolSyncError("Invalid interaction request");
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
				pendingInteractionGeneration: record.generation,
				totalPendingInteractionCount: state.pendingInteractions.some(
					(request) => isRecord(request) && request.id === incomingRequest.id,
				)
					? state.totalPendingInteractionCount
					: state.totalPendingInteractionCount + 1,
			};
		}
		case "ui_resolved": {
			if (
				typeof record.generation !== "number" ||
				!Number.isSafeInteger(record.generation) ||
				record.generation !== state.pendingInteractionGeneration + 1
			) {
				throw new ProtocolSyncError("Interaction generation mismatch");
			}
			const requestId = record.requestId;
			if (typeof requestId !== "string") {
				throw new ProtocolSyncError("Invalid interaction resolution");
			}
			return {
				...state,
				pendingInteractions: state.pendingInteractions.filter(
					(request) => !isRecord(request) || request.id !== requestId,
				),
				pendingInteractionGeneration: record.generation,
				totalPendingInteractionCount: Math.max(
					0,
					state.totalPendingInteractionCount - 1,
				),
			};
		}
		case "session.transcript.replaced":
			throw new ProtocolRebaseRequired("The transcript changed");
		case "agent.run.failed":
		case "error":
			return {
				...state,
				activeTurnId: null,
				pendingStatus: null,
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
			if (
				typeof record.pendingInteractionGeneration !== "number" ||
				!Number.isSafeInteger(record.pendingInteractionGeneration) ||
				record.pendingInteractionGeneration < 0
			) {
				throw new ProtocolSyncError("Invalid pending interaction generation");
			}
			const messages = Array.isArray(record.messages) ? record.messages : [];
			const chrome =
				record.chrome === undefined
					? EMPTY_REMOTE_CHROME
					: parseRemoteChromeSnapshot(record.chrome);
			if (!chrome) {
				throw new ProtocolSyncError("Invalid chrome contribution snapshot");
			}
			const finalMessage = messages.at(-1);
			const activeMessageIndex =
				serverState.isStreaming === true &&
				isRecord(finalMessage) &&
				finalMessage.role === "assistant"
					? messages.length - 1
					: null;
			const activeTurnId =
				serverState.isStreaming === true &&
				isRecord(finalMessage) &&
				typeof finalMessage.turnId === "string"
					? finalMessage.turnId
					: null;
			const messageQueue = messageQueueState(
				{
					count: serverState.pendingMessageCount,
					generation: serverState.pendingMessageGeneration,
					previews: serverState.pendingMessagePreviews,
				},
				"snapshot",
			);
			if (!messageQueue) {
				throw new ProtocolSyncError("Invalid queued message snapshot");
			}
			return {
				...state,
				phase: "synchronizing",
				streamId: record.streamId,
				sequence: record.sequence,
				serverState,
				chrome,
				messages,
				messageKeys: messages.map((message, index) =>
					messageKey(
						message,
						`message:${(typeof record.messageOffset === "number" ? record.messageOffset : 0) + index}`,
					),
				),
				messageOffset:
					typeof record.messageOffset === "number" ? record.messageOffset : 0,
				totalMessageCount:
					typeof record.totalMessageCount === "number"
						? record.totalMessageCount
						: messages.length,
				activeMessageIndex,
				activeTurnId,
				pendingActiveMessageDeltas: [],
				tools: [],
				pendingStatus: serverState.isStreaming === true ? "Working…" : null,
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
				pendingInteractionGeneration: record.pendingInteractionGeneration,
				queuedMessageCount: messageQueue.count,
				queuedMessageGeneration: messageQueue.generation ?? 0,
				queuedMessagePreviews: messageQueue.previews,
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
