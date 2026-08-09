import { describe, expect, test } from "bun:test";
import { WebRemoteServices } from "./remote-services";

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

	test("omits empty command arguments", async () => {
		const seen: Record<string, unknown>[] = [];
		const services = new WebRemoteServices({
			command: async (command) => {
				seen.push(command);
				return {};
			},
		});

		await services.executeCommand("session.list", "   ", 4);
		await services.executeCommand("session.open", "session-1", 4);
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
			},
		]);
	});
});
