import { describe, expect, test } from "bun:test";
import { RemoteSessionServices } from "./remote-services";

describe("remote session configuration services", () => {
	test("validates models and thinking levels", async () => {
		const services = new RemoteSessionServices({
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
		const services = new RemoteSessionServices({
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
		const services = new RemoteSessionServices({
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

describe("remote session recovery services", () => {
	test("hydrates unpadded non-ASCII message chunks", async () => {
		const message = {
			id: "message-1",
			role: "assistant",
			content: "terminé",
		};
		const bytes = Buffer.from(JSON.stringify(message));
		const offsets: number[] = [];
		const services = new RemoteSessionServices({
			command: async (command) => {
				expect(command).toMatchObject({
					type: "get_message_chunk",
					token: "message-token",
				});
				if (typeof command.offset !== "number") {
					throw new Error("Message chunk omitted its offset");
				}
				offsets.push(command.offset);
				const nextOffset = Math.min(command.offset + 5, bytes.length);
				return {
					data: {
						token: "message-token",
						encoding: "base64-json",
						data: bytes
							.subarray(command.offset, nextOffset)
							.toString("base64")
							.replace(/=+$/, ""),
						offset: command.offset,
						nextOffset,
						totalBytes: bytes.length,
						complete: nextOffset === bytes.length,
					},
				};
			},
		});

		await expect(
			services.resolveMessageReference({
				type: "message_reference",
				token: "message-token",
			}),
		).resolves.toEqual(message);
		expect(offsets.length).toBeGreaterThan(1);
	});
});

describe("remote session scratchpad services", () => {
	test("loads and updates session scratchpad content", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new RemoteSessionServices({
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

describe("remote session queued follow-up services", () => {
	test("restores and promotes with session and generation guards", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new RemoteSessionServices({
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

describe("remote session review services", () => {
	test("validates review summaries", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new RemoteSessionServices({
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
		const services = new RemoteSessionServices({
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

describe("remote session command services", () => {
	test("validates and returns transport-neutral commands", async () => {
		const services = new RemoteSessionServices({
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
		const services = new RemoteSessionServices({
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
		const services = new RemoteSessionServices({
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
		const services = new RemoteSessionServices({
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
