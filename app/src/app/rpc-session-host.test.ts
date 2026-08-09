import { describe, expect, test } from "bun:test";
import { createCommandRegistry } from "../features/commands";
import type { AgentRuntime, AgentRuntimeEvent } from "../runtime/agent-runtime";
import type { KitAgentMessage, Turn } from "../session/types";
import { RemoteAttachmentStore } from "./remote-attachment-store";
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

		expect(first).toEqual([{ type: "agent.start" }]);
		expect(second).toEqual([{ type: "agent.start" }]);
		host.dispose();
	});

	test("projects identified Kit message events without Pi stream payloads", () => {
		const runtime = createRuntime();
		const host = new RpcSessionHost(runtime);
		const records: unknown[] = [];
		host.subscribe((record) => records.push(record));
		const turn: Turn = { id: "turn-1", messages: [] };
		const message = {
			role: "assistant",
			content: [{ type: "text", text: "hi" }],
			api: "anthropic-messages",
			provider: "anthropic",
			model: "claude-sonnet-4-6",
			usage: {
				input: 1,
				output: 1,
				cacheRead: 0,
				cacheWrite: 0,
				totalTokens: 2,
				cost: {
					input: 0,
					output: 0,
					cacheRead: 0,
					cacheWrite: 0,
					total: 0,
				},
			},
			stopReason: "stop",
			timestamp: 1,
			messageId: "message-1",
			turnId: "turn-1",
		} as Extract<KitAgentMessage, { role: "assistant" }>;

		runtime.emit({
			type: "agent.message.started",
			turn,
			message,
		} as AgentRuntimeEvent);
		runtime.emit({
			type: "agent.message.updated",
			turn,
			message,
			update: {
				kind: "content.delta",
				contentType: "text",
				contentIndex: 0,
				delta: "hi",
			},
		} as AgentRuntimeEvent);
		runtime.emit({
			type: "session.message.appended",
			session: runtime.getSession(),
			turn,
			message,
		} as AgentRuntimeEvent);
		runtime.emit({
			type: "session.handoff_summary.appended",
			session: runtime.getSession(),
			summaryMessage: message,
		} as AgentRuntimeEvent);
		runtime.emit({
			type: "session.transcript.replaced",
			session: runtime.getSession(),
			reason: "recovery",
			removedMessageId: "message-1",
		} as AgentRuntimeEvent);

		expect(records).toEqual([
			{
				type: "agent.message.started",
				turnId: "turn-1",
				messageId: "message-1",
				message,
			},
			{
				type: "agent.message.updated",
				turnId: "turn-1",
				messageId: "message-1",
				update: {
					kind: "content.delta",
					contentType: "text",
					contentIndex: 0,
					delta: "hi",
				},
			},
			{
				type: "session.message.appended",
				turnId: "turn-1",
				messageId: "message-1",
				message,
			},
			{
				type: "session.handoff_summary.appended",
				turnId: "turn-1",
				messageId: "message-1",
				message,
			},
			{
				type: "session.transcript.replaced",
				reason: "recovery",
				removedMessageId: "message-1",
			},
		]);
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
					protocolVersion: 2,
					interactiveUI: false,
					attachmentReferences: false,
					maxAttachmentsPerPrompt: 0,
					limits: {
						attachments: {
							maxFiles: 0,
							maxFilesPerPrompt: 0,
							maxFileBytes: 0,
							maxTextFileBytes: 0,
							maxTotalBytes: 0,
							maxPromptBytes: 0,
							maxPromptTextBytes: 0,
							maxConcurrentUploads: 0,
						},
						pagination: {
							messages: { defaultPageSize: 100, maxPageSize: 200 },
							pendingInteractions: {
								defaultPageSize: 20,
								maxPageSize: 50,
							},
						},
						recovery: {
							pendingInteraction: {
								maxChunkBytes: 48 * 1024,
								maxTotalBytes: 2 * 1024 * 1024,
							},
						},
					},
					eventSequencing: { supported: false },
				}),
			}),
		]);
		const response = responses[0] as { data: { commands: string[] } };
		expect(response.data.commands).not.toContain("ui_response");
		host.dispose();
	});

	test("advertises attachment quotas when references are enabled", async () => {
		const host = new RpcSessionHost(createRuntime(), {
			attachments: new RemoteAttachmentStore(),
		});
		const responses: unknown[] = [];
		await host.handleCommand(
			{ id: "capabilities", type: "get_capabilities" },
			async (record) => {
				responses.push(record);
			},
		);

		expect(responses).toEqual([
			expect.objectContaining({
				data: expect.objectContaining({
					attachmentReferences: true,
					limits: expect.objectContaining({
						attachments: {
							maxFiles: 32,
							maxFilesPerPrompt: 8,
							maxFileBytes: 10 * 1024 * 1024,
							maxTextFileBytes: 1024 * 1024,
							maxTotalBytes: 50 * 1024 * 1024,
							maxPromptBytes: 20 * 1024 * 1024,
							maxPromptTextBytes: 1024 * 1024,
							maxConcurrentUploads: 0,
						},
					}),
				}),
			}),
		]);
		host.dispose();
	});

	test("paginates transcript snapshots on request", async () => {
		const messages = Array.from({ length: 5 }, (_, index) => ({
			role: "user",
			content: `message-${index}`,
			messageId: `message-${index}`,
			turnId: `turn-${index}`,
		}));
		const host = new RpcSessionHost(
			createRuntime({ getMessages: () => messages }),
		);
		const responses: unknown[] = [];
		await host.handleCommand(
			{ id: "messages", type: "get_messages", offset: 2, limit: 2 },
			async (record) => {
				responses.push(record);
			},
		);

		expect(responses).toEqual([
			expect.objectContaining({
				id: "messages",
				data: {
					messages: messages.slice(2, 4),
					offset: 2,
					totalMessageCount: 5,
					hasMore: true,
				},
			}),
		]);
		host.dispose();
	});

	test("submits uploaded attachments and consumes their opaque ids", async () => {
		const attachments = new RemoteAttachmentStore();
		const attachment = await attachments.add(
			new File(["hello from a file"], "notes.txt", { type: "text/plain" }),
		);
		const submissions: unknown[] = [];
		const host = new RpcSessionHost(
			createRuntime({
				submitUserMessage: async (input: unknown, onAccepted?: () => void) => {
					submissions.push(input);
					onAccepted?.();
				},
			}),
			{ attachments },
		);
		const responses: unknown[] = [];
		await host.handleCommand(
			{
				id: "prompt",
				type: "prompt",
				message: "Review this",
				attachmentIds: [attachment.id],
			},
			async (record) => {
				responses.push(record);
			},
		);
		await Bun.sleep(0);

		expect(responses[0]).toEqual(
			expect.objectContaining({ id: "prompt", success: true }),
		);
		expect(submissions).toEqual([
			[
				{ type: "text", text: "Review this" },
				{
					type: "text",
					text: '<uploaded_file filename="notes.txt" mime_type="text/plain">\nhello from a file\n</uploaded_file>',
				},
			],
		]);
		await host.handleCommand(
			{
				id: "reuse",
				type: "prompt",
				attachmentIds: [attachment.id],
			},
			async (record) => {
				responses.push(record);
			},
		);
		expect(responses.at(-1)).toEqual(
			expect.objectContaining({
				id: "reuse",
				success: false,
				error: `Attachment is unavailable: ${attachment.id}`,
			}),
		);
		host.dispose();
	});

	test("releases attachment claims when prompt submission fails", async () => {
		const attachments = new RemoteAttachmentStore();
		const attachment = await attachments.add(new File(["retry"], "retry.txt"));
		let attempts = 0;
		const host = new RpcSessionHost(
			createRuntime({
				submitUserMessage: async (_input: unknown, onAccepted?: () => void) => {
					attempts += 1;
					if (attempts === 1) throw new Error("submission failed");
					onAccepted?.();
				},
			}),
			{ attachments },
		);
		const events: unknown[] = [];
		host.subscribe((event) => events.push(event));
		await host.handleCommand(
			{ id: "first", type: "prompt", attachmentIds: [attachment.id] },
			async () => {},
		);
		await Bun.sleep(0);
		await host.handleCommand(
			{ id: "retry", type: "prompt", attachmentIds: [attachment.id] },
			async () => {},
		);
		await Bun.sleep(0);

		expect(attempts).toBe(2);
		expect(events).toContainEqual({
			type: "error",
			error: "submission failed",
		});
		host.dispose();
	});

	test("pages and chunks pending remote interactions", async () => {
		const interactions = new RemoteInteractionBroker();
		const abortController = new AbortController();
		const pending = interactions.confirm({
			title: "Confirm",
			message: "x".repeat(20 * 1024),
			signal: abortController.signal,
		});
		const request = interactions.getPendingRequests()[0];
		if (!request) throw new Error("Expected pending interaction");
		const host = new RpcSessionHost(createRuntime(), { interactions });
		const responses: unknown[] = [];
		await host.handleCommand(
			{ id: "pending", type: "get_pending_interactions", limit: 1 },
			async (record) => {
				responses.push(record);
			},
		);
		await host.handleCommand(
			{
				id: "chunk",
				type: "get_pending_interaction_chunk",
				requestId: request.id,
			},
			async (record) => {
				responses.push(record);
			},
		);

		expect(responses[0]).toEqual(
			expect.objectContaining({
				data: {
					requests: [request],
					offset: 0,
					generation: 1,
					stale: false,
					totalRequestCount: 1,
					hasMore: false,
				},
			}),
		);
		const chunk = responses[1] as { data: { data: string; complete: boolean } };
		expect(chunk.data.complete).toBe(true);
		expect(
			JSON.parse(Buffer.from(chunk.data.data, "base64").toString()),
		).toEqual(request);
		abortController.abort();
		expect(await pending).toBe(false);
		host.dispose();
	});

	test("enforces the advertised interaction recovery limit", async () => {
		const interactions = new RemoteInteractionBroker();
		const abortController = new AbortController();
		const pending = interactions.confirm({
			title: "Oversized",
			message: "x".repeat(2 * 1024 * 1024),
			signal: abortController.signal,
		});
		const request = interactions.getPendingRequests()[0];
		if (!request) throw new Error("Expected pending interaction");
		const host = new RpcSessionHost(createRuntime(), { interactions });
		const responses: unknown[] = [];
		await host.handleCommand(
			{
				id: "oversized",
				type: "get_pending_interaction_chunk",
				requestId: request.id,
			},
			async (record) => {
				responses.push(record);
			},
		);

		expect(responses).toEqual([
			expect.objectContaining({
				id: "oversized",
				success: false,
				error: `Serialized value exceeds ${2 * 1024 * 1024} bytes`,
			}),
		]);
		abortController.abort();
		expect(await pending).toBe(false);
		host.dispose();
	});

	test("rejects pending interaction pages from a stale generation", async () => {
		const interactions = new RemoteInteractionBroker();
		const firstAbort = new AbortController();
		const secondAbort = new AbortController();
		const first = interactions.confirm({
			title: "First",
			signal: firstAbort.signal,
		});
		const generation = interactions.getPendingSnapshot().generation;
		const second = interactions.confirm({
			title: "Second",
			signal: secondAbort.signal,
		});
		const host = new RpcSessionHost(createRuntime(), { interactions });
		const responses: unknown[] = [];
		await host.handleCommand(
			{
				id: "stale",
				type: "get_pending_interactions",
				generation,
			},
			async (record) => {
				responses.push(record);
			},
		);

		expect(responses).toEqual([
			expect.objectContaining({
				data: {
					requests: [],
					offset: 0,
					generation: generation + 1,
					stale: true,
					totalRequestCount: 2,
					hasMore: false,
				},
			}),
		]);
		firstAbort.abort();
		secondAbort.abort();
		expect(await first).toBe(false);
		expect(await second).toBe(false);
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
					registryGeneration: 0,
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

	test("rejects transport-neutral commands from a stale registry generation", async () => {
		let executed = false;
		const commands = createCommandRegistry([
			{
				name: "plugin.toggle",
				description: "Toggle the plugin",
				execute: () => {},
				executeTransportNeutral: async () => {
					executed = true;
				},
			},
		]);
		const host = new RpcSessionHost(createRuntime(), { commands });
		commands.register({
			name: "plugin.other",
			description: "Another command",
			execute: () => {},
			executeTransportNeutral: async () => {},
		});
		const responses: Array<Record<string, unknown>> = [];

		await host.handleCommand(
			{
				id: "execute",
				type: "execute_command",
				commandId: "plugin.toggle",
				registryGeneration: 0,
			},
			async (record) => {
				responses.push(record as Record<string, unknown>);
			},
		);

		expect(executed).toBe(false);
		expect(responses).toEqual([
			expect.objectContaining({
				id: "execute",
				success: false,
				error: "Command registry changed; refresh commands",
			}),
		]);
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
