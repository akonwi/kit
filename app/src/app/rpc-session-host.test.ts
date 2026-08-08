import { describe, expect, test } from "bun:test";
import { createCommandRegistry } from "../features/commands";
import type { AgentRuntime, AgentRuntimeEvent } from "../runtime/agent-runtime";
import { RemoteInteractionBroker } from "./remote-interaction-broker";
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
				data: expect.objectContaining({
					protocolVersion: 1,
					interactiveUI: false,
				}),
			}),
		]);
		const response = responses[0] as { data: { commands: string[] } };
		expect(response.data.commands).not.toContain("ui_response");
		host.dispose();
	});

	test("routes remote UI responses and rejects late responders", async () => {
		const interactions = new RemoteInteractionBroker();
		const host = new RpcSessionHost(createRuntime(), { interactions });
		const events: Array<Record<string, unknown>> = [];
		const responses: unknown[] = [];
		host.subscribe((record) => events.push(record as Record<string, unknown>));
		const disconnect = host.connectClient(() => {});
		const capabilities: unknown[] = [];
		await host.handleCommand(
			{ id: "capabilities", type: "get_capabilities" },
			async (record) => {
				capabilities.push(record);
			},
		);
		expect(capabilities).toEqual([
			expect.objectContaining({
				data: expect.objectContaining({
					interactiveUI: true,
					commands: expect.arrayContaining(["ui_response"]),
					interactionKinds: ["confirm", "input", "select", "guided_questions"],
				}),
			}),
		]);

		const confirmation = interactions.confirm({ title: "Proceed?" });
		const request = events.find((event) => event.type === "ui_request");
		if (!request || typeof request.request !== "object" || !request.request) {
			throw new Error("Expected UI request");
		}
		const requestId = (request.request as { id: string }).id;
		await host.handleCommand(
			{
				id: "ui-1",
				type: "ui_response",
				requestId,
				response: { confirmed: true },
			},
			async (record) => {
				responses.push(record);
			},
		);
		await host.handleCommand(
			{
				id: "ui-2",
				type: "ui_response",
				requestId,
				response: { confirmed: false },
			},
			async (record) => {
				responses.push(record);
			},
		);

		expect(await confirmation).toBe(true);
		expect(responses).toEqual([
			expect.objectContaining({ id: "ui-1", success: true }),
			expect.objectContaining({
				id: "ui-2",
				success: false,
				error: "Interaction is no longer pending",
			}),
		]);
		expect(events.at(-1)).toMatchObject({
			type: "ui_resolved",
			requestId,
			resolution: "answered",
		});
		disconnect();
		host.dispose();
	});

	test("executes transport-neutral commands without blocking UI responses", async () => {
		const interactions = new RemoteInteractionBroker();
		let receivedArgs = "";
		const commands = createCommandRegistry([
			{
				name: "local-only",
				description: "Needs the TUI",
				execute: () => {},
			},
			{
				name: "plugin.toggle",
				displayName: "toggle",
				description: "Toggle the plugin",
				execute: () => {},
				executeTransportNeutral: async (args) => {
					receivedArgs = args;
					await interactions.confirm({ title: "Enable?" });
				},
			},
		]);
		const host = new RpcSessionHost(createRuntime(), {
			interactions,
			commands,
		});
		const events: Array<Record<string, unknown>> = [];
		const responses: unknown[] = [];
		host.subscribe((record) => events.push(record as Record<string, unknown>));
		const disconnect = host.connectClient(() => {});

		await host.handleCommand(
			{ id: "list", type: "list_commands" },
			async (record) => {
				responses.push(record);
			},
		);
		const execution = host.handleCommand(
			{
				id: "execute",
				type: "execute_command",
				commandId: "plugin.toggle",
				args: "on",
			},
			async (record) => {
				responses.push(record);
			},
		);
		await Bun.sleep(0);
		const request = events.find((event) => event.type === "ui_request");
		if (!request || typeof request.request !== "object" || !request.request) {
			throw new Error("Expected UI request");
		}
		const requestId = (request.request as { id: string }).id;
		const interactionResponse = host.handleCommand(
			{
				id: "answer",
				type: "ui_response",
				requestId,
				response: { confirmed: true },
			},
			async (record) => {
				responses.push(record);
			},
		);
		await Promise.all([execution, interactionResponse]);

		expect(receivedArgs).toBe("on");
		expect(responses[0]).toEqual(
			expect.objectContaining({
				id: "list",
				data: {
					commands: [
						{
							id: "plugin.toggle",
							name: "toggle",
							description: "Toggle the plugin",
							argName: undefined,
							category: undefined,
						},
					],
				},
			}),
		);
		expect(responses).toEqual(
			expect.arrayContaining([
				expect.objectContaining({ id: "answer", success: true }),
				expect.objectContaining({ id: "execute", success: true }),
			]),
		);
		disconnect();
		host.dispose();
	});

	test("aborts a hung transport-neutral command out of band", async () => {
		const commands = createCommandRegistry([
			{
				name: "plugin.hang",
				description: "Never finishes",
				execute: () => {},
				executeTransportNeutral: () => new Promise<void>(() => {}),
			},
		]);
		const host = new RpcSessionHost(createRuntime(), {
			commands,
			commandCancellationGraceMs: 1,
		});
		const responses: unknown[] = [];
		const execution = host.handleCommand(
			{
				id: "execute",
				type: "execute_command",
				commandId: "plugin.hang",
			},
			async (record) => {
				responses.push(record);
			},
		);
		await Bun.sleep(0);
		const queued = host.handleCommand(
			{ id: "queued", type: "get_state" },
			async (record) => {
				responses.push(record);
			},
		);
		const abort = host.handleCommand(
			{ id: "abort", type: "abort" },
			async (record) => {
				responses.push(record);
			},
		);
		await Promise.all([execution, queued, abort]);

		expect(responses).toEqual(
			expect.arrayContaining([
				expect.objectContaining({
					id: "execute",
					success: false,
					error: "Command execution aborted",
				}),
				expect.objectContaining({
					id: "queued",
					success: false,
					error: "Command cancelled by abort",
				}),
				expect.objectContaining({ id: "abort", success: true }),
			]),
		);
		await host.handleCommand(
			{ id: "after", type: "get_state" },
			async (record) => {
				responses.push(record);
			},
		);
		expect(responses.at(-1)).toEqual(
			expect.objectContaining({
				id: "after",
				success: false,
				error: "A cancelled command did not stop; restart the RPC host",
			}),
		);
		await host.abortAndWait();
		host.dispose();
	});

	test("lists sessions and opens them by opaque id after workspace plugins are ready", async () => {
		let switchedTo = "";
		let finishWorkspaceTransition: (() => void) | undefined;
		const sessions = [
			{
				id: "session-2",
				cwd: "/workspace",
				createdAt: "2026-01-01T00:00:00.000Z",
				updatedAt: "2026-01-01T00:00:00.000Z",
				messageCount: 3,
			},
		];
		const host = new RpcSessionHost(
			createRuntime({
				listAllSessions: async () => sessions,
				switchSessionById: async (id: string) => {
					switchedTo = id;
					return true;
				},
				waitForModelAdaptation: async () => {},
			}),
			{
				waitForWorkspaceReady: () =>
					new Promise<void>((resolve) => {
						finishWorkspaceTransition = resolve;
					}),
			},
		);
		const responses: unknown[] = [];
		await host.handleCommand(
			{ id: "list", type: "list_sessions" },
			async (record) => {
				responses.push(record);
			},
		);
		const opened = host.handleCommand(
			{ id: "open", type: "open_session", sessionId: "session-2" },
			async (record) => {
				responses.push(record);
			},
		);
		await Bun.sleep(0);
		expect(responses).toHaveLength(1);
		finishWorkspaceTransition?.();
		await opened;

		expect(responses[0]).toEqual(
			expect.objectContaining({ data: { sessions } }),
		);
		expect(responses[1]).toEqual(
			expect.objectContaining({ id: "open", success: true }),
		);
		expect(switchedTo).toBe("session-2");
		host.dispose();
	});

	test("can disable legacy path-based session switching", async () => {
		let switched = false;
		const host = new RpcSessionHost(
			createRuntime({
				switchSession: async () => {
					switched = true;
					return true;
				},
			}),
			{ allowLegacySessionPaths: false },
		);
		const responses: unknown[] = [];
		await host.handleCommand(
			{
				id: "legacy",
				type: "switch_session",
				sessionPath: "../../session.jsonl",
			},
			async (record) => {
				responses.push(record);
			},
		);

		expect(switched).toBe(false);
		expect(responses).toEqual([
			expect.objectContaining({
				id: "legacy",
				success: false,
				error: "Legacy session paths are unavailable",
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
