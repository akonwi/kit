import { describe, expect, test } from "bun:test";
import { PassThrough } from "node:stream";
import type { AgentRuntime, AgentRuntimeEvent } from "../runtime/agent-runtime";
import { RpcModeServer } from "./rpc-mode";

function createRuntime(overrides: Record<string, unknown> = {}) {
	let listener: ((event: AgentRuntimeEvent) => void) | undefined;
	const runtime = {
		subscribe: (next: (event: AgentRuntimeEvent) => void) => {
			listener = next;
			return () => {
				listener = undefined;
			};
		},
		getStatus: () => ({ isStreaming: false }),
		getSession: () => ({ id: "session-1", cwd: "/workspace" }),
		getCurrentModel: () => undefined,
		agentInfo: { thinkingLevel: "off" },
		getMessages: () => [],
		getPendingMessageCount: () => 0,
		getPendingMessageGeneration: () => 0,
		getPendingMessages: () => [],
		abort: () => listener?.({ type: "agent.start" } as AgentRuntimeEvent),
		submitUserMessage: async () => {},
		...overrides,
	} as unknown as AgentRuntime;
	return runtime;
}

async function runProtocol(inputText: string): Promise<unknown[]> {
	const input = new PassThrough();
	const records: unknown[] = [];
	const server = new RpcModeServer(createRuntime(), input, async (record) => {
		records.push(record);
	});
	input.end(inputText);
	await server.start();
	server.dispose();
	return records;
}

describe("RPC mode protocol", () => {
	test("accepts CRLF and a final unterminated command", async () => {
		const records = await runProtocol(
			'{"id":"state","type":"get_state"}\r\n{"id":"messages","type":"get_messages"}',
		);

		expect(records).toEqual([
			{
				id: "state",
				type: "response",
				command: "get_state",
				success: true,
				data: {
					thinkingLevel: "off",
					isStreaming: false,
					contextUsage: null,
					sessionId: "session-1",
					cwd: "/workspace",
					messageCount: 0,
					pendingMessageCount: 0,
					pendingMessageGeneration: 0,
					pendingMessagePreviews: [],
				},
			},
			{
				id: "messages",
				type: "response",
				command: "get_messages",
				success: true,
				data: { messages: [] },
			},
		]);
	});

	test("keeps malformed and unknown commands non-fatal", async () => {
		const records = await runProtocol(
			'not-json\n{"id":"unknown-1","type":"unknown"}\n',
		);

		expect(records).toHaveLength(2);
		expect(records[0]).toMatchObject({
			type: "response",
			command: "parse",
			success: false,
		});
		expect(records[1]).toEqual({
			id: "unknown-1",
			type: "response",
			command: "unknown",
			success: false,
			error: "Unknown command: unknown",
		});
	});

	test("forwards runtime events independently of command responses", async () => {
		const records = await runProtocol('{"id":"abort-1","type":"abort"}\n');

		expect(records).toEqual([
			{ type: "agent.start" },
			{
				id: "abort-1",
				type: "response",
				command: "abort",
				success: true,
			},
		]);
	});

	test("acknowledges a prompt before synchronously emitted run events", async () => {
		let emit: ((event: AgentRuntimeEvent) => void) | undefined;
		const runtime = createRuntime({
			subscribe: (listener: (event: AgentRuntimeEvent) => void) => {
				emit = listener;
				return () => {};
			},
			submitUserMessage: async () => {
				emit?.({ type: "agent.start" } as AgentRuntimeEvent);
			},
		});
		const input = new PassThrough();
		const records: unknown[] = [];
		const server = new RpcModeServer(runtime, input, async (record) => {
			records.push(record);
		});
		input.end('{"id":"prompt-1","type":"prompt","message":"hello"}\n');

		await server.start();
		expect(records).toEqual([
			{
				id: "prompt-1",
				type: "response",
				command: "prompt",
				success: true,
			},
			{ type: "agent.start" },
		]);
	});

	test("reserves an accepted prompt before processing another prompt", async () => {
		let finishRun: (() => void) | undefined;
		const runtime = createRuntime({
			submitUserMessage: () =>
				new Promise<void>((resolve) => {
					finishRun = resolve;
				}),
		});
		const input = new PassThrough();
		const records: unknown[] = [];
		const server = new RpcModeServer(runtime, input, async (record) => {
			records.push(record);
		});
		input.end(
			'{"id":"one","type":"prompt","message":"first"}\n{"id":"two","type":"prompt","message":"second"}\n',
		);

		await server.start();
		expect(records).toEqual([
			{
				id: "one",
				type: "response",
				command: "prompt",
				success: true,
			},
			{
				id: "two",
				type: "response",
				command: "prompt",
				success: false,
				error:
					'Agent is streaming; set streamingBehavior to "steer" or "followUp"',
			},
		]);
		finishRun?.();
		await server.abortAndWait();
	});

	test("rejects model changes while an accepted prompt is running", async () => {
		let finishRun: (() => void) | undefined;
		let modelChanged = false;
		const model = { provider: "test", id: "model-1" };
		const runtime = createRuntime({
			getAvailableModels: () => [model],
			setModel: () => {
				modelChanged = true;
			},
			submitUserMessage: () =>
				new Promise<void>((resolve) => {
					finishRun = resolve;
				}),
		});
		const input = new PassThrough();
		const records: unknown[] = [];
		const server = new RpcModeServer(runtime, input, async (record) => {
			records.push(record);
		});
		input.end(
			'{"id":"prompt-1","type":"prompt","message":"hello"}\n{"id":"model-1","type":"set_model","provider":"test","modelId":"model-1"}\n',
		);

		await server.start();
		expect(modelChanged).toBe(false);
		expect(records.at(-1)).toEqual({
			id: "model-1",
			type: "response",
			command: "set_model",
			success: false,
			error: "Cannot change models while the agent is streaming",
		});
		finishRun?.();
		await server.abortAndWait();
	});

	test("waits for model adaptation before dispatching the next command", async () => {
		let finishAdaptation: (() => void) | undefined;
		let promptSubmitted = false;
		const model = { provider: "test", id: "model-1" };
		const runtime = createRuntime({
			getAvailableModels: () => [model],
			setModel: () => {},
			waitForModelAdaptation: () =>
				new Promise<void>((resolve) => {
					finishAdaptation = resolve;
				}),
			submitUserMessage: async () => {
				promptSubmitted = true;
			},
		});
		const input = new PassThrough();
		const records: unknown[] = [];
		const server = new RpcModeServer(runtime, input, async (record) => {
			records.push(record);
		});
		input.end(
			'{"id":"model-1","type":"set_model","provider":"test","modelId":"model-1"}\n{"id":"prompt-1","type":"prompt","message":"hello"}\n',
		);

		const started = server.start();
		await Bun.sleep(0);
		expect(promptSubmitted).toBe(false);
		expect(records).toEqual([]);

		finishAdaptation?.();
		await started;
		expect(promptSubmitted).toBe(true);
		expect(records).toEqual([
			{
				id: "model-1",
				type: "response",
				command: "set_model",
				success: true,
				data: model,
			},
			{
				id: "prompt-1",
				type: "response",
				command: "prompt",
				success: true,
			},
		]);
	});

	test("waits for project plugins after switching sessions", async () => {
		let finishPlugins: (() => void) | undefined;
		let promptSubmitted = false;
		const runtime = createRuntime({
			switchSession: async () => true,
			waitForModelAdaptation: async () => {},
			submitUserMessage: async () => {
				promptSubmitted = true;
			},
		});
		const input = new PassThrough();
		const records: unknown[] = [];
		const server = new RpcModeServer(
			runtime,
			input,
			async (record) => {
				records.push(record);
			},
			false,
			undefined,
			() =>
				new Promise<void>((resolve) => {
					finishPlugins = resolve;
				}),
		);
		input.end(
			'{"id":"switch-1","type":"switch_session","sessionPath":"session-2"}\n{"id":"prompt-1","type":"prompt","message":"hello"}\n',
		);

		const started = server.start();
		await Bun.sleep(0);
		expect(promptSubmitted).toBe(false);
		expect(records).toEqual([]);

		finishPlugins?.();
		await started;
		expect(promptSubmitted).toBe(true);
		expect(records.map((record) => (record as { id?: string }).id)).toEqual([
			"switch-1",
			"prompt-1",
		]);
	});

	test("emits Kit semantic lifecycle boundaries", async () => {
		let emit: ((event: AgentRuntimeEvent) => void) | undefined;
		const user = {
			role: "user",
			content: "hello",
			timestamp: 1,
			messageId: "message-user",
			turnId: "turn-1",
		};
		const assistant = {
			role: "assistant",
			content: [{ type: "text", text: "hi" }],
			stopReason: "stop",
			timestamp: 2,
			messageId: "message-assistant",
			turnId: "turn-1",
		};
		const turn = { id: "turn-1", messages: [user, assistant] };
		const runtime = createRuntime({
			subscribe: (listener: (event: AgentRuntimeEvent) => void) => {
				emit = listener;
				return () => {};
			},
			submitUserMessage: async () => {
				for (const event of [
					{ type: "agent.start" },
					{ type: "agent.turn.started", turn },
					{ type: "user.message.created", turn, message: user },
					{
						type: "session.message.appended",
						session: runtime.getSession(),
						turn,
						message: user,
					},
					{ type: "agent.message.started", turn, message: assistant },
					{
						type: "agent.message.updated",
						turn,
						message: assistant,
						update: {
							kind: "content.delta",
							contentType: "text",
							contentIndex: 0,
							delta: "hi",
						},
					},
					{ type: "agent.message.ended", turn, message: assistant },
					{
						type: "session.message.appended",
						session: runtime.getSession(),
						turn,
						message: assistant,
					},
					{ type: "agent.end", messages: [user, assistant], willRetry: false },
					{ type: "agent.turn.completed", turn },
					{ type: "agent.settled" },
				]) {
					emit?.(event as AgentRuntimeEvent);
				}
			},
		});
		const input = new PassThrough();
		const records: Array<{ type?: string }> = [];
		const server = new RpcModeServer(runtime, input, async (record) => {
			records.push(record as { type?: string });
		});
		input.end('{"id":"prompt-1","type":"prompt","message":"hello"}\n');

		await server.start();
		expect(records.map((record) => record.type)).toEqual([
			"response",
			"agent.start",
			"agent.turn.started",
			"user.message.created",
			"session.message.appended",
			"agent.message.started",
			"agent.message.updated",
			"agent.message.ended",
			"session.message.appended",
			"agent.end",
			"agent.turn.completed",
			"state_changed",
			"agent.settled",
		]);
	});
});
