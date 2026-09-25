import { randomUUID } from "node:crypto";
import {
	type SessionClient,
	SessionCommandRejectedError,
} from "@akonwi/kit-session-client";
import { type MessagePart, messagePartToPromptText } from "../messages/parts";
import type { AgentRuntime } from "../runtime/agent-runtime";

export type ComposerMessagePart = {
	type: string;
	text?: string;
	review?: unknown;
	data?: string;
	mimeType?: string;
	filename?: string;
	sourcePath?: string;
};

export type ComposerMessage = {
	role: string;
	content?: string | ComposerMessagePart[];
	command?: string;
	excludeFromContext?: boolean;
	timestamp?: number;
};

export type ComposerSessionState = {
	id: string;
	cwd: string;
	isStreaming: boolean;
	bashExecution: boolean;
	pendingMessageCount: number;
	pendingMessageGeneration: number;
	messages: readonly ComposerMessage[];
};

export type RestoredComposerMessages = {
	messages: ComposerMessage[];
	acknowledge(): Promise<void>;
};

export interface ComposerSession {
	state(): ComposerSessionState;
	submitMessage(parts: MessagePart[]): Promise<void>;
	executeBash(command: string, excludeFromContext: boolean): Promise<void>;
	sendFollowUp(message: string): void | Promise<void>;
	restorePendingMessages(): Promise<RestoredComposerMessages>;
	promotePendingFollowUpsToSteering(): void | Promise<void>;
	abort(): void | Promise<void>;
	quit(): void;
}

export function createRuntimeComposerSession(
	runtime: AgentRuntime,
): ComposerSession {
	return {
		state: () => {
			const session = runtime.getSession();
			return {
				id: session.id,
				cwd: session.cwd,
				isStreaming: runtime.getStatus().isStreaming,
				bashExecution: true,
				pendingMessageCount: runtime.getPendingMessageCount(),
				pendingMessageGeneration: runtime.getPendingMessageGeneration(),
				messages: runtime.getMessages(),
			};
		},
		submitMessage: (parts) => runtime.submitMessage(parts),
		executeBash: (command, excludeFromContext) =>
			runtime.executeBash(command, excludeFromContext),
		sendFollowUp: (message) => runtime.sendFollowUp(message),
		restorePendingMessages: async () => ({
			messages: runtime.drainPendingMessages(),
			acknowledge: async () => {},
		}),
		promotePendingFollowUpsToSteering: () =>
			runtime.promotePendingFollowUpsToSteering(),
		abort: () => runtime.abort(),
		quit: () => runtime.quit(),
	};
}

function remoteText(parts: MessagePart[]): string {
	const unsupported = parts.find((part) => part.type !== "text");
	if (unsupported) {
		throw new Error(
			`Remote composer does not yet support ${unsupported.type} attachments`,
		);
	}
	return parts
		.map((part) => messagePartToPromptText(part))
		.filter((text) => text.trim())
		.join("\n");
}

function toComposerMessagePart(value: unknown): ComposerMessagePart | null {
	if (typeof value !== "object" || value === null || Array.isArray(value)) {
		return null;
	}
	const candidate = value as {
		type?: unknown;
		text?: unknown;
		data?: unknown;
		mimeType?: unknown;
		filename?: unknown;
		sourcePath?: unknown;
	};
	if (typeof candidate.type !== "string") return null;
	return {
		type: candidate.type,
		...(typeof candidate.text === "string" ? { text: candidate.text } : {}),
		...(typeof candidate.data === "string" ? { data: candidate.data } : {}),
		...(typeof candidate.mimeType === "string"
			? { mimeType: candidate.mimeType }
			: {}),
		...(typeof candidate.filename === "string"
			? { filename: candidate.filename }
			: {}),
		...(typeof candidate.sourcePath === "string"
			? { sourcePath: candidate.sourcePath }
			: {}),
	};
}

function toComposerMessage(value: unknown): ComposerMessage | null {
	if (typeof value !== "object" || value === null || Array.isArray(value)) {
		return null;
	}
	const candidate = value as {
		role?: unknown;
		content?: unknown;
		command?: unknown;
		excludeFromContext?: unknown;
		timestamp?: unknown;
	};
	if (typeof candidate.role !== "string") return null;
	const content = Array.isArray(candidate.content)
		? candidate.content
				.map(toComposerMessagePart)
				.filter((part): part is ComposerMessagePart => part !== null)
		: typeof candidate.content === "string"
			? candidate.content
			: undefined;
	return {
		role: candidate.role,
		...(content === undefined ? {} : { content }),
		...(typeof candidate.command === "string"
			? { command: candidate.command }
			: {}),
		...(typeof candidate.excludeFromContext === "boolean"
			? { excludeFromContext: candidate.excludeFromContext }
			: {}),
		...(typeof candidate.timestamp === "number"
			? { timestamp: candidate.timestamp }
			: {}),
	};
}

export function createSessionClientComposerSession(
	client: SessionClient,
	onQuit: () => void,
): ComposerSession {
	const mutationClientId = randomUUID();
	let lastCwd: string | null = null;
	let bashOperation: {
		id: string;
		command: string;
		excludeFromContext: boolean;
	} | null = null;
	const pendingBashAcknowledgements = new Set<string>();
	let restoreOperation: {
		id: string;
		sessionId: string;
		expectedGeneration: number;
		messages: ComposerMessage[] | null;
		applied: boolean;
	} | null = null;
	return {
		state: () => {
			const state = client.state;
			if (typeof state.serverState.cwd === "string") {
				lastCwd = state.serverState.cwd;
			}
			return {
				id: client.sessionId,
				cwd: lastCwd ?? "",
				isStreaming: state.serverState.isStreaming === true,
				bashExecution: client.services.supportsCommand("execute_bash"),
				pendingMessageCount: state.queuedMessageCount,
				pendingMessageGeneration: state.queuedMessageGeneration,
				messages: state.messages
					.map(toComposerMessage)
					.filter((message): message is ComposerMessage => message !== null),
			};
		},
		submitMessage: async (parts) => {
			const message = remoteText(parts);
			const streaming = client.state.serverState.isStreaming === true;
			await client.command({
				type: "prompt",
				message,
				...(streaming ? { streamingBehavior: "followUp" } : {}),
			});
		},
		executeBash: async (command, excludeFromContext) => {
			for (const operationId of [...pendingBashAcknowledgements]) {
				try {
					await client.command({
						type: "acknowledge_bash_execution",
						clientId: mutationClientId,
						operationId,
					});
					pendingBashAcknowledgements.delete(operationId);
				} catch {
					break;
				}
			}
			if (
				bashOperation &&
				(bashOperation.command !== command ||
					bashOperation.excludeFromContext !== excludeFromContext)
			) {
				throw new Error(
					"The previous bash acceptance is unknown; retry that command first",
				);
			}
			bashOperation ??= { id: randomUUID(), command, excludeFromContext };
			const operation = bashOperation;
			try {
				await client.command({
					type: "execute_bash",
					clientId: mutationClientId,
					operationId: operation.id,
					command,
					excludeFromContext,
				});
			} catch (error) {
				if (error instanceof SessionCommandRejectedError) bashOperation = null;
				throw error;
			}
			bashOperation = null;
			try {
				await client.command({
					type: "acknowledge_bash_execution",
					clientId: mutationClientId,
					operationId: operation.id,
				});
			} catch {
				pendingBashAcknowledgements.add(operation.id);
			}
		},
		sendFollowUp: async (message) => {
			await client.command({ type: "follow_up", message });
		},
		restorePendingMessages: async () => {
			const state = client.state;
			if (!restoreOperation) {
				if (state.queuedMessageCount === 0) {
					return { messages: [], acknowledge: async () => {} };
				}
				restoreOperation = {
					id: randomUUID(),
					sessionId: client.sessionId,
					expectedGeneration: state.queuedMessageGeneration,
					messages: null,
					applied: false,
				};
			}
			const operation = restoreOperation;
			if (!operation.messages) {
				const result = await client.services.restoreFollowUps(
					mutationClientId,
					operation.id,
					operation.sessionId,
					operation.expectedGeneration,
				);
				operation.messages = result.messages.map((content) => ({
					role: "user",
					content,
				}));
			}
			return {
				messages: operation.applied ? [] : operation.messages,
				acknowledge: async () => {
					operation.applied = true;
					try {
						await client.services.acknowledgeFollowUpMutation(
							mutationClientId,
							operation.id,
						);
					} catch {
						await client.services.acknowledgeFollowUpMutation(
							mutationClientId,
							operation.id,
						);
					}
					if (restoreOperation === operation) restoreOperation = null;
				},
			};
		},
		promotePendingFollowUpsToSteering: async () => {
			const state = client.state;
			await client.services.promoteFollowUps(
				client.sessionId,
				state.queuedMessageGeneration,
			);
		},
		abort: async () => {
			await client.command({ type: "abort" });
		},
		quit: onQuit,
	};
}
