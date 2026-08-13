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
