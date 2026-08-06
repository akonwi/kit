import type { Readable } from "node:stream";
import type { ThinkingLevel } from "../runtime/agent";
import type { AgentRuntime, AgentRuntimeEvent } from "../runtime/agent-runtime";
import { getAvailableThinkingLevels } from "../runtime/thinking-levels";
import {
	createSession,
	findSessionById,
	readSession,
	type Session,
	writeSession,
} from "../session";
import {
	createEphemeralSession,
	createHeadlessHost,
	takeOverStdout,
} from "./headless-host";

type RpcCommand = {
	id?: string;
	type: string;
	[key: string]: unknown;
};

type RpcResponse = {
	id?: string;
	type: "response";
	command: string;
	success: boolean;
	data?: unknown;
	error?: string;
};

type RpcWriter = (record: unknown) => Promise<void>;

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requireString(command: RpcCommand, key: string): string {
	const value = command[key];
	if (typeof value !== "string" || !value.trim()) {
		throw new Error(`${key} must be a non-empty string`);
	}
	return value;
}

function assistantText(message: unknown): string | null {
	if (!isRecord(message) || message.role !== "assistant") return null;
	if (!Array.isArray(message.content)) return null;
	const text = message.content
		.filter(
			(block): block is { type: "text"; text: string } =>
				isRecord(block) &&
				block.type === "text" &&
				typeof block.text === "string",
		)
		.map((block) => block.text)
		.join("\n");
	return text || null;
}

function eventRecord(event: AgentRuntimeEvent): unknown[] {
	switch (event.type) {
		case "agent.start":
			return [{ type: "agent_start" }];
		case "turn.start":
			return [{ type: "turn_start" }];
		case "message.start":
			return [{ type: "message_start", message: event.message }];
		case "message.update":
			return [
				{
					type: "message_update",
					assistantMessageEvent: event.assistantMessageEvent,
				},
			];
		case "message.end":
			return [{ type: "message_end", message: event.message }];
		case "agent.tool.started":
			return [
				{
					type: "tool_execution_start",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
				},
			];
		case "agent.tool.updated":
			return [
				{
					type: "tool_execution_update",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
					partialResult: event.partialResult,
				},
			];
		case "agent.tool.ended":
			return [
				{
					type: "tool_execution_end",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					result: event.result,
					isError: event.isError,
				},
			];
		case "agent.turn.ended":
			return [
				{
					type: "turn_end",
					message: event.message,
					toolResults: event.toolResults,
				},
			];
		case "agent.end":
			return [
				{
					type: "agent_end",
					messages: event.messages,
					willRetry: event.willRetry ?? false,
				},
			];
		case "agent.settled":
			return [{ type: "agent_settled" }];
		case "chat.message-queue.changed":
			return [
				{
					type: "queue_update",
					steering: event.steering,
					followUp: event.messages,
				},
			];
		case "agent.retry.started":
			return [
				{
					type: "auto_retry_start",
					attempt: event.attempt,
					maxAttempts: event.maxAttempts,
					delayMs: event.delayMs,
				},
			];
		case "agent.retry.failed":
			return [
				{
					type: "auto_retry_end",
					success: false,
					attempt: event.attempt,
					finalError: event.error,
				},
			];
		case "agent.retry.completed":
			return [
				{
					type: "auto_retry_end",
					success: true,
					attempt: event.attempt,
				},
			];
		case "agent.run.failed":
			return [{ type: "error", error: event.error }];
		default:
			return [];
	}
}

export class RpcModeServer {
	private writeQueue = Promise.resolve();
	private commandQueue = Promise.resolve();
	private readonly acceptedRuns = new Set<Promise<void>>();
	private unsubscribe: (() => void) | null = null;
	private promptReserved = false;

	constructor(
		private readonly runtime: AgentRuntime,
		private readonly input: Readable,
		private readonly writeRecord: RpcWriter,
		private readonly persistSessions = false,
	) {}

	async start(): Promise<void> {
		this.unsubscribe = this.runtime.subscribe((event) => {
			for (const record of eventRecord(event)) void this.write(record);
		});

		let buffer = Buffer.alloc(0);
		for await (const chunk of this.input) {
			const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
			buffer = Buffer.concat([buffer, bytes]);
			let newline = buffer.indexOf(0x0a);
			while (newline >= 0) {
				const line = buffer.subarray(0, newline);
				buffer = buffer.subarray(newline + 1);
				this.processLine(line.at(-1) === 0x0d ? line.subarray(0, -1) : line);
				newline = buffer.indexOf(0x0a);
			}
		}
		if (buffer.length > 0) {
			this.processLine(
				buffer.at(-1) === 0x0d ? buffer.subarray(0, -1) : buffer,
			);
		}
		await this.commandQueue;
		await this.writeQueue;
	}

	dispose(): void {
		this.unsubscribe?.();
		this.unsubscribe = null;
	}

	async abortAndWait(): Promise<void> {
		this.runtime.abort();
		await Promise.allSettled(this.acceptedRuns);
		await this.writeQueue;
	}

	private processLine(line: Buffer): void {
		if (line.length === 0) return;
		let parsed: unknown;
		try {
			parsed = JSON.parse(line.toString("utf8"));
		} catch (error) {
			void this.write({
				type: "response",
				command: "parse",
				success: false,
				error: `Failed to parse command: ${error instanceof Error ? error.message : String(error)}`,
			});
			return;
		}
		if (!isRecord(parsed) || typeof parsed.type !== "string") {
			void this.write({
				type: "response",
				command: "parse",
				success: false,
				error: "Command must be an object with a string type",
			});
			return;
		}
		const command = parsed as RpcCommand;
		this.commandQueue = this.commandQueue.then(() =>
			this.handleCommand(command).catch((error) =>
				this.respond(command, false, undefined, error),
			),
		);
	}

	private async handleCommand(command: RpcCommand): Promise<void> {
		switch (command.type) {
			case "prompt": {
				const message = requireString(command, "message");
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					if (command.streamingBehavior === "steer") {
						this.runtime.sendSteer(message);
					} else if (command.streamingBehavior === "followUp") {
						this.runtime.sendFollowUp(message);
					} else {
						throw new Error(
							'Agent is streaming; set streamingBehavior to "steer" or "followUp"',
						);
					}
					await this.respond(command, true);
					return;
				}
				this.promptReserved = true;
				await this.respond(command, true);
				const run = this.runtime
					.submitUserMessage(message)
					.catch((error) =>
						this.write({
							type: "error",
							error: error instanceof Error ? error.message : String(error),
						}),
					)
					.finally(() => {
						this.promptReserved = false;
						this.acceptedRuns.delete(run);
					});
				this.acceptedRuns.add(run);
				return;
			}
			case "steer":
				this.runtime.sendSteer(requireString(command, "message"));
				await this.respond(command, true);
				return;
			case "follow_up":
				this.runtime.sendFollowUp(requireString(command, "message"));
				await this.respond(command, true);
				return;
			case "abort":
				this.runtime.abort();
				await Promise.allSettled(this.acceptedRuns);
				await this.respond(command, true);
				return;
			case "new_session":
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Cannot create a session while the agent is streaming",
					);
				}
				await this.runtime.newSession();
				await this.runtime.waitForModelAdaptation();
				if (this.persistSessions) await writeSession(this.runtime.getSession());
				await this.respond(command, true, { cancelled: false });
				return;
			case "get_state": {
				const session = this.runtime.getSession();
				await this.respond(command, true, {
					model: this.runtime.getCurrentModel(),
					thinkingLevel: this.runtime.agentInfo.thinkingLevel,
					isStreaming: this.runtime.getStatus().isStreaming,
					sessionId: session.id,
					sessionName: session.name,
					cwd: session.cwd,
					messageCount: this.runtime.getMessages().length,
					pendingMessageCount: this.runtime.getPendingMessageCount(),
				});
				return;
			}
			case "get_messages":
				await this.respond(command, true, {
					messages: this.runtime.getMessages(),
				});
				return;
			case "get_last_assistant_text": {
				const message = this.runtime
					.getMessages()
					.findLast((candidate) => candidate.role === "assistant");
				await this.respond(command, true, { text: assistantText(message) });
				return;
			}
			case "get_available_models":
				await this.respond(command, true, {
					models: this.runtime.getAvailableModels(),
				});
				return;
			case "set_model": {
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error("Cannot change models while the agent is streaming");
				}
				const provider = requireString(command, "provider");
				const modelId = requireString(command, "modelId");
				const model = this.runtime
					.getAvailableModels()
					.find(
						(candidate) =>
							candidate.provider === provider && candidate.id === modelId,
					);
				if (!model) throw new Error(`Model not found: ${provider}/${modelId}`);
				this.runtime.setModel(model);
				await this.runtime.waitForModelAdaptation();
				await this.respond(command, true, model);
				return;
			}
			case "get_available_thinking_levels":
				await this.respond(command, true, {
					levels: getAvailableThinkingLevels(this.runtime.getCurrentModel()),
				});
				return;
			case "set_thinking_level": {
				const level = requireString(command, "level") as ThinkingLevel;
				const levels = getAvailableThinkingLevels(
					this.runtime.getCurrentModel(),
				);
				if (!levels.includes(level)) {
					throw new Error(`Thinking level not available: ${level}`);
				}
				this.runtime.setThinkingLevel(level);
				await this.respond(command, true);
				return;
			}
			case "switch_session": {
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Cannot switch sessions while the agent is streaming",
					);
				}
				const switched = await this.runtime.switchSession(
					requireString(command, "sessionPath"),
				);
				if (!switched) throw new Error("Session not found");
				await this.runtime.waitForModelAdaptation();
				await this.respond(command, true, { cancelled: false });
				return;
			}
			default:
				throw new Error(`Unknown command: ${command.type}`);
		}
	}

	private respond(
		command: RpcCommand,
		success: boolean,
		data?: unknown,
		error?: unknown,
	): Promise<void> {
		const response: RpcResponse = {
			...(command.id === undefined ? {} : { id: command.id }),
			type: "response",
			command: command.type,
			success,
			...(data === undefined ? {} : { data }),
			...(error === undefined
				? {}
				: { error: error instanceof Error ? error.message : String(error) }),
		};
		return this.write(response);
	}

	private write(record: unknown): Promise<void> {
		this.writeQueue = this.writeQueue.then(() => this.writeRecord(record));
		return this.writeQueue;
	}
}

async function resolveSession(
	cwd: string,
	options: { noSession: boolean; sessionId?: string },
): Promise<{ session: Session; persistSession: boolean }> {
	if (options.noSession) {
		return { session: createEphemeralSession(cwd), persistSession: false };
	}
	if (options.sessionId) {
		const session =
			(await findSessionById(options.sessionId)) ??
			(await readSession(options.sessionId));
		if (!session) throw new Error(`Session not found: ${options.sessionId}`);
		return { session, persistSession: true };
	}
	const session = await createSession(cwd);
	await writeSession(session);
	return { session, persistSession: true };
}

export async function runRpcMode(
	cwd: string,
	options: { noSession?: boolean; sessionId?: string } = {},
): Promise<number> {
	const stdout = takeOverStdout();
	let host: Awaited<ReturnType<typeof createHeadlessHost>> | null = null;
	let server: RpcModeServer | null = null;
	let signalExitCode: number | null = null;
	const handleSignal = (signal: "SIGINT" | "SIGTERM") => {
		const exitCode = signal === "SIGINT" ? 130 : 143;
		if (signalExitCode !== null) process.exit(exitCode);
		signalExitCode = exitCode;
		host?.runtime.abort();
		process.stdin.destroy();
	};
	const handleSigint = () => handleSignal("SIGINT");
	const handleSigterm = () => handleSignal("SIGTERM");
	process.on("SIGINT", handleSigint);
	process.on("SIGTERM", handleSigterm);
	let exitCode = 0;
	try {
		const resolved = await resolveSession(cwd, {
			noSession: options.noSession ?? false,
			sessionId: options.sessionId,
		});
		host = await createHeadlessHost(resolved.session, {
			persistSession: resolved.persistSession,
		});
		server = new RpcModeServer(
			host.runtime,
			process.stdin,
			(record) => stdout.write(`${JSON.stringify(record)}\n`),
			resolved.persistSession,
		);
		await server.start();
		await server.abortAndWait();
		exitCode = signalExitCode ?? 0;
	} catch (error) {
		if (signalExitCode !== null) {
			exitCode = signalExitCode;
		} else {
			console.error(error instanceof Error ? error.message : String(error));
			exitCode = 1;
		}
	} finally {
		process.off("SIGINT", handleSigint);
		process.off("SIGTERM", handleSigterm);
		server?.dispose();
		try {
			await host?.dispose();
		} catch (error) {
			console.error(
				`Headless cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
			);
			if (signalExitCode === null) exitCode = 1;
		} finally {
			stdout.restore();
		}
	}
	return exitCode;
}
