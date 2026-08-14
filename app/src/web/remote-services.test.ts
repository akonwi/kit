import { describe, expect, test } from "bun:test";
import { WebRemoteServices } from "./remote-services";

describe("web remote configuration services", () => {
	test("validates models and thinking levels", async () => {
		const services = new WebRemoteServices({
			command: async (command) => {
				if (command.type === "get_available_models") {
					return {
						data: {
							models: [{ id: "model-1", provider: "test", name: "Model One" }],
						},
					};
				}
				if (command.type === "get_available_thinking_levels") {
					return { data: { levels: ["off", "high"] } };
				}
				return {};
			},
		});

		await expect(services.listModels()).resolves.toEqual([
			{ id: "model-1", provider: "test", name: "Model One" },
		]);
		await expect(services.listThinkingLevels()).resolves.toEqual([
			"off",
			"high",
		]);
	});

	test("accepts capabilities from hosts without queued follow-up limits", async () => {
		const services = new WebRemoteServices({
			command: async () => ({
				data: {
					limits: {
						attachments: {},
						pagination: {
							messages: {},
							pendingInteractions: {},
						},
						recovery: {
							message: {},
							pendingInteraction: {},
						},
					},
				},
			}),
		});

		await expect(services.fetchLimits()).resolves.toMatchObject({
			maxFollowUpDraftBytes: 1024 * 1024,
			maxFollowUpDraftItems: 128,
			maxPendingFollowUpMutations: 16,
		});
	});

	test("sends model and thinking-level selections", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new WebRemoteServices({
			command: async (command) => {
				seen.push(command);
				return {};
			},
		});

		await services.setModel({ id: "model-1", provider: "test" });
		await services.setThinkingLevel("high");
		expect(seen).toEqual([
			{ type: "set_model", provider: "test", modelId: "model-1" },
			{ type: "set_thinking_level", level: "high" },
		]);
	});
});

describe("web remote attachment services", () => {
	test("keeps the global receiver when using browser fetch", async () => {
		const originalFetch = globalThis.fetch;
		let receiver: unknown;
		globalThis.fetch = function (this: typeof globalThis) {
			receiver = this;
			return Promise.resolve(
				Response.json({ attachment: { id: "attachment-1" } }, { status: 201 }),
			);
		} as unknown as typeof fetch;
		const services = new WebRemoteServices({ command: async () => ({}) });
		globalThis.fetch = originalFetch;

		await expect(
			services.uploadAttachment(new File(["image"], "screenshot.png")),
		).resolves.toBe("attachment-1");
		expect(receiver).toBe(globalThis);
	});
});

describe("web remote scratchpad services", () => {
	test("loads and updates session scratchpad content", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new WebRemoteServices({
			command: async (command) => {
				seen.push(command);
				return { data: { sessionId: "session-1", content: "notes" } };
			},
		});

		await expect(services.getScratchpad()).resolves.toEqual({
			sessionId: "session-1",
			content: "notes",
		});
		await expect(
			services.updateScratchpad("session-1", "old", "notes"),
		).resolves.toEqual({ sessionId: "session-1", content: "notes" });
		expect(seen).toEqual([
			{ type: "get_scratchpad" },
			{
				type: "update_scratchpad",
				sessionId: "session-1",
				expectedContent: "old",
				content: "notes",
			},
		]);
	});
});

describe("web remote queued follow-up services", () => {
	test("restores and promotes with session and generation guards", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new WebRemoteServices({
			command: async (command) => {
				seen.push(command);
				if (command.type === "restore_follow_ups") {
					return {
						data: {
							clientId: "client-a",
							operationId: "operation-restore",
							sessionId: "session-1",
							generation: 6,
							messages: ["first", "second"],
						},
					};
				}
				if (command.type === "acknowledge_follow_up_mutation") {
					return {
						data: {
							clientId: "client-a",
							operationId: "operation-restore",
							acknowledged: true,
						},
					};
				}
				return {
					data: {
						sessionId: "session-1",
						generation: 7,
						count: 2,
					},
				};
			},
		});

		await expect(
			services.restoreFollowUps(
				"client-a",
				"operation-restore",
				"session-1",
				5,
			),
		).resolves.toEqual({
			clientId: "client-a",
			operationId: "operation-restore",
			sessionId: "session-1",
			generation: 6,
			messages: ["first", "second"],
		});
		await expect(services.promoteFollowUps("session-1", 6)).resolves.toEqual({
			sessionId: "session-1",
			generation: 7,
			count: 2,
		});
		await expect(
			services.acknowledgeFollowUpMutation("client-a", "operation-restore"),
		).resolves.toBeTrue();
		expect(seen).toEqual([
			{
				type: "restore_follow_ups",
				clientId: "client-a",
				operationId: "operation-restore",
				sessionId: "session-1",
				expectedGeneration: 5,
			},
			{
				type: "promote_follow_ups",
				sessionId: "session-1",
				expectedGeneration: 6,
			},
			{
				type: "acknowledge_follow_up_mutation",
				clientId: "client-a",
				operationId: "operation-restore",
			},
		]);
	});
});

describe("web remote review services", () => {
	test("validates review summaries", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new WebRemoteServices({
			command: async (command) => {
				seen.push(command);
				return {
					data: {
						sessionId: "session-1",
						generation: 4,
						repoRoot: "/workspace",
						files: [
							{
								id: "src/a.ts",
								path: "src/a.ts",
								status: "change",
								source: "working",
								additions: 1,
								deletions: 0,
								changeCount: 1,
							},
						],
					},
				};
			},
		});

		await expect(services.getReviewState()).resolves.toMatchObject({
			generation: 4,
			files: [{ path: "src/a.ts" }],
		});
		expect(seen).toEqual([{ type: "get_review_state" }]);
	});

	test("submits review notes with session and generation guards", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new WebRemoteServices({
			command: async (command) => {
				seen.push(command);
				return {};
			},
		});
		const notes = [
			{
				path: "src/a.ts",
				side: "additions" as const,
				startLine: 2,
				endLine: 3,
				comment: "Check this range",
			},
		];

		await services.submitReview("submission-1", "session-1", 4, notes);
		expect(seen).toEqual([
			{
				type: "submit_review",
				submissionId: "submission-1",
				sessionId: "session-1",
				generation: 4,
				notes,
			},
		]);
	});
});

describe("web remote command services", () => {
	test("validates and returns transport-neutral commands", async () => {
		const services = new WebRemoteServices({
			command: async (command) => {
				expect(command).toEqual({ type: "list_commands" });
				return {
					data: {
						registryGeneration: 3,
						commands: [
							{
								id: "session.new",
								name: "New session",
								description: "Start a new session",
								argName: "cwd",
								category: "Session",
							},
						],
					},
				};
			},
		});

		await expect(services.listCommands()).resolves.toEqual({
			registryGeneration: 3,
			commands: [
				{
					id: "session.new",
					name: "New session",
					description: "Start a new session",
					argName: "cwd",
					category: "Session",
				},
			],
		});
	});

	test("rejects malformed command records", async () => {
		const services = new WebRemoteServices({
			command: async () => ({
				data: {
					registryGeneration: 0,
					commands: [{ name: "Missing id" }],
				},
			}),
		});

		await expect(services.listCommands()).rejects.toThrow(
			"Command list contains an invalid command",
		);
	});

	test("activates chrome contributions by area and stable id", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new WebRemoteServices({
			command: async (command) => {
				seen.push(command);
				return {};
			},
		});

		await services.activateChromeContribution("header", "speech.status");
		expect(seen).toEqual([
			{
				type: "activate_chrome_contribution",
				area: "header",
				contributionId: "speech.status",
			},
		]);
	});

	test("serializes optional command arguments and session preconditions", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new WebRemoteServices({
			command: async (command) => {
				seen.push(command);
				return {};
			},
		});

		await services.executeCommand("session.list", "   ", 4);
		await services.executeCommand("session.open", "session-1", 4, "active-1");
		expect(seen).toEqual([
			{
				type: "execute_command",
				commandId: "session.list",
				registryGeneration: 4,
			},
			{
				type: "execute_command",
				commandId: "session.open",
				registryGeneration: 4,
				args: "session-1",
				expectedSessionId: "active-1",
			},
		]);
	});
});
