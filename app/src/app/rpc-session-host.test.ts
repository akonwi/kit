import { describe, expect, test } from "bun:test";
import type { AgentRuntime, AgentRuntimeEvent } from "../runtime/agent-runtime";
import { RpcSessionHost } from "./rpc-session-host";

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
		abort: () => {},
		submitUserMessage: async () => {},
		emit: (event: AgentRuntimeEvent) => listener?.(event),
		...overrides,
	} as unknown as AgentRuntime & {
		emit(event: AgentRuntimeEvent): void;
	};
	return runtime;
}

describe("RpcSessionHost", () => {
	test("broadcasts runtime events to every subscriber", () => {
		const runtime = createRuntime();
		const host = new RpcSessionHost(runtime);
		const first: unknown[] = [];
		const second: unknown[] = [];
		host.subscribe((record) => first.push(record));
		host.subscribe((record) => second.push(record));

		runtime.emit({ type: "agent.start" } as AgentRuntimeEvent);

		expect(first).toEqual([{ type: "agent_start" }]);
		expect(second).toEqual([{ type: "agent_start" }]);
		host.dispose();
	});

	test("broadcasts state snapshots for runtime state changes", () => {
		const runtime = createRuntime();
		const host = new RpcSessionHost(runtime);
		const records: unknown[] = [];
		host.subscribe((record) => records.push(record));

		runtime.emit({
			type: "session.model.changed",
			session: runtime.getSession(),
			modelId: "model-1",
		} as AgentRuntimeEvent);

		expect(records).toEqual([
			{
				type: "state_changed",
				state: expect.objectContaining({ sessionId: "session-1" }),
			},
		]);
		host.dispose();
	});

	test("reports protocol capabilities", async () => {
		const host = new RpcSessionHost(createRuntime());
		const responses: unknown[] = [];
		await host.handleCommand(
			{ id: "capabilities", type: "get_capabilities" },
			async (record) => {
				responses.push(record);
			},
		);

		expect(responses).toEqual([
			expect.objectContaining({
				id: "capabilities",
				success: true,
				data: expect.objectContaining({ protocolVersion: 1 }),
			}),
		]);
		host.dispose();
	});

	test("keeps response correlation scoped to each caller", async () => {
		const host = new RpcSessionHost(createRuntime());
		const first: unknown[] = [];
		const second: unknown[] = [];

		await Promise.all([
			host.handleCommand({ id: "first", type: "get_state" }, async (record) => {
				first.push(record);
			}),
			host.handleCommand(
				{ id: "second", type: "get_messages" },
				async (record) => {
					second.push(record);
				},
			),
		]);

		expect(first).toEqual([
			expect.objectContaining({ id: "first", command: "get_state" }),
		]);
		expect(second).toEqual([
			expect.objectContaining({ id: "second", command: "get_messages" }),
		]);
		host.dispose();
	});

	test("serializes state-changing commands from different callers", async () => {
		let finishAdaptation: (() => void) | undefined;
		const model = { provider: "test", id: "model-1" };
		const runtime = createRuntime({
			getAvailableModels: () => [model],
			setModel: () => {},
			waitForModelAdaptation: () =>
				new Promise<void>((resolve) => {
					finishAdaptation = resolve;
				}),
		});
		const host = new RpcSessionHost(runtime);
		const responses: string[] = [];

		const first = host.handleCommand(
			{
				id: "model",
				type: "set_model",
				provider: "test",
				modelId: "model-1",
			},
			async () => {
				responses.push("model");
			},
		);
		const second = host.handleCommand(
			{ id: "state", type: "get_state" },
			async () => {
				responses.push("state");
			},
		);

		await Bun.sleep(0);
		expect(responses).toEqual([]);
		finishAdaptation?.();
		await Promise.all([first, second]);
		expect(responses).toEqual(["model", "state"]);
		host.dispose();
	});

	test("does not launch an acknowledged prompt after shutdown starts", async () => {
		let releaseResponse: (() => void) | undefined;
		let submitted = false;
		const host = new RpcSessionHost(
			createRuntime({
				submitUserMessage: async () => {
					submitted = true;
				},
			}),
		);
		const command = host.handleCommand(
			{ id: "prompt", type: "prompt", message: "hello" },
			() =>
				new Promise<void>((resolve) => {
					releaseResponse = resolve;
				}),
		);

		await Bun.sleep(0);
		const shutdown = host.abortAndWait();
		releaseResponse?.();
		await Promise.all([command, shutdown]);

		expect(submitted).toBe(false);
		host.dispose();
	});

	test("drains the active command and rejects queued work during shutdown", async () => {
		let finishAdaptation: (() => void) | undefined;
		const model = { provider: "test", id: "model-1" };
		const runtime = createRuntime({
			getAvailableModels: () => [model],
			setModel: () => {},
			waitForModelAdaptation: () =>
				new Promise<void>((resolve) => {
					finishAdaptation = resolve;
				}),
		});
		const host = new RpcSessionHost(runtime);
		const queuedResponses: unknown[] = [];
		void host.handleCommand(
			{
				id: "model",
				type: "set_model",
				provider: "test",
				modelId: "model-1",
			},
			async () => {},
		);
		void host.handleCommand(
			{ id: "queued", type: "get_state" },
			async (record) => {
				queuedResponses.push(record);
			},
		);

		await Bun.sleep(0);
		const shutdown = host.abortAndWait();
		finishAdaptation?.();
		await shutdown;

		expect(queuedResponses).toEqual([
			expect.objectContaining({
				id: "queued",
				success: false,
				error: "RPC host is shutting down",
			}),
		]);
		host.dispose();
	});
});
