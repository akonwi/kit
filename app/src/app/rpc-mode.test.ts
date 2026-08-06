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
					sessionId: "session-1",
					cwd: "/workspace",
					messageCount: 0,
					pendingMessageCount: 0,
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
			{ type: "agent_start" },
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
			{ type: "agent_start" },
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

	test("emits balanced Pi-style lifecycle boundaries", async () => {
		let emit: ((event: AgentRuntimeEvent) => void) | undefined;
		const user = { role: "user", content: "hello", timestamp: 1 };
		const assistant = {
			role: "assistant",
			content: [{ type: "text", text: "hi" }],
			stopReason: "stop",
			timestamp: 2,
		};
		const runtime = createRuntime({
			subscribe: (listener: (event: AgentRuntimeEvent) => void) => {
				emit = listener;
				return () => {};
			},
			submitUserMessage: async () => {
				for (const event of [
					{ type: "agent.start" },
					{ type: "turn.start" },
					{ type: "message.start", message: user },
					{ type: "message.end", message: user },
					{ type: "message.start", message: assistant },
					{
						type: "message.update",
						message: assistant,
						assistantMessageEvent: {
							type: "text_delta",
							contentIndex: 0,
							delta: "hi",
						},
					},
					{ type: "message.end", message: assistant },
					{
						type: "agent.turn.ended",
						turn: null,
						message: assistant,
						toolResults: [],
					},
					{ type: "agent.end", messages: [user, assistant], willRetry: false },
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
			"agent_start",
			"turn_start",
			"message_start",
			"message_end",
			"message_start",
			"message_update",
			"message_end",
			"turn_end",
			"agent_end",
			"agent_settled",
		]);
	});
});
