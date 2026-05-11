import { describe, expect, test } from "bun:test";
import type { Command } from "../features/commands";
import { PluginManager, type PluginRegistration } from "./PluginManager";
import type { PluginContext } from "./types";

function createPluginContext(commands: Command[]): PluginContext {
	return {
		runtime: {} as PluginContext["runtime"],
		commands: {
			register(command: Command): () => void {
				commands.push(command);
				return () => {
					const index = commands.indexOf(command);
					if (index >= 0) commands.splice(index, 1);
				};
			},
			getAll: () => [...commands],
		},
		settings: { settings: {}, paths: {} as PluginContext["settings"]["paths"] },
		ui: {
			toast: () => {},
			custom: async () => undefined as never,
			getTranscriptViewport: () => null,
		},
		attachments: {} as PluginContext["attachments"],
	};
}

describe("PluginManager", () => {
	test("continues after non-fatal plugin errors and cleans partial registrations", () => {
		const commands: Command[] = [];
		const errors: Array<{ name: string; error: unknown }> = [];
		const badPlugin: PluginRegistration = {
			name: "bad",
			continueOnError: true,
			onError: (error) => errors.push(error),
			initialize: (kit) => {
				kit.registerCommand("bad", { description: "Bad command" }, () => {});
				throw new Error("boom");
			},
		};

		function GoodPlugin(kit: Parameters<PluginRegistration["initialize"]>[0]) {
			kit.registerCommand("good", { description: "Good command" }, () => {});
		}

		const manager = new PluginManager(
			[badPlugin, GoodPlugin],
			createPluginContext(commands),
		);
		manager.initialize();

		expect(errors).toHaveLength(1);
		expect(errors[0]?.name).toBe("bad");
		expect(errors[0]?.error).toBeInstanceOf(Error);
		expect(commands.map((command) => command.name)).toEqual(["good"]);
	});

	test("reports external contribution conflicts as non-fatal plugin errors", () => {
		const commands: Command[] = [
			{
				name: "taken",
				description: "Already registered",
				execute: () => {},
			},
		];
		const errors: Array<{ name: string; error: unknown }> = [];
		const plugin: PluginRegistration = {
			name: "external:taken",
			continueOnError: true,
			checkContributionConflicts: true,
			onError: (error) => errors.push(error),
			initialize: (kit) => {
				kit.registerCommand("taken", { description: "Duplicate" }, () => {});
			},
		};

		const manager = new PluginManager([plugin], createPluginContext(commands));
		manager.initialize();

		expect(errors).toHaveLength(1);
		expect(errors[0]?.name).toBe("external:taken");
		expect(errors[0]?.error).toBeInstanceOf(Error);
		expect(commands.map((command) => command.name)).toEqual(["taken"]);
	});
});
