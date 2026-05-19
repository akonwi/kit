import { describe, expect, test } from "bun:test";
import type { Command } from "../features/commands";
import type { ToolApprovalHandler } from "../runtime/agent-runtime";
import { PluginManager, type PluginRegistration } from "./PluginManager";
import type { PluginContext } from "./types";

function createPluginContext(
	commands: Command[],
	runtime: Partial<PluginContext["runtime"]> = {},
): PluginContext {
	return {
		runtime: runtime as PluginContext["runtime"],
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
			select: async () => undefined,
			input: async () => undefined,
			confirm: async () => false,
			custom: async () => undefined as never,
			getTranscriptViewport: () => null,
		},
		attachments: {} as PluginContext["attachments"],
		footer: {
			setContribution: () => {},
			clearContribution: () => {},
			clearNamespace: () => {},
			hideContribution: () => () => {},
			getContributions: () => [],
			subscribe: () => () => {},
		},
		header: {
			setContribution: () => {},
			clearContribution: () => {},
			clearNamespace: () => {},
			hideContribution: () => () => {},
			getContributions: () => [],
			subscribe: () => () => {},
		},
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

	test("limits external plugin ui to public primitives", () => {
		let uiKeys: string[] = [];
		const plugin: PluginRegistration = {
			name: "external:ui",
			initialize: (kit) => {
				uiKeys = Object.keys(kit.ui).sort();
			},
		};

		const manager = new PluginManager([plugin], createPluginContext([]));
		manager.initialize();

		expect(uiKeys).toEqual(["confirm", "input", "select", "toast"]);
	});

	test("exposes internal ui to built-in plugins", () => {
		let uiKeys: string[] = [];
		const plugin: PluginRegistration = {
			name: "internal:ui",
			internalUi: true,
			initialize: (kit) => {
				uiKeys = Object.keys(kit.ui).sort();
			},
		};

		const manager = new PluginManager([plugin], createPluginContext([]));
		manager.initialize();

		expect(uiKeys).toEqual([
			"confirm",
			"custom",
			"getTranscriptViewport",
			"input",
			"select",
			"toast",
		]);
	});

	test("registers tool call handlers with public ui", async () => {
		let approvalHandler: ToolApprovalHandler | undefined;
		let disposed = false;
		let uiKeys: string[] = [];
		const runtime = {
			addToolApprovalHandler(handler: ToolApprovalHandler) {
				approvalHandler = handler;
				return () => {
					disposed = true;
				};
			},
		} satisfies Partial<PluginContext["runtime"]>;
		const plugin: PluginRegistration = {
			name: "external:approval",
			initialize: (kit) => {
				kit.onToolCall((toolCall, ctx) => {
					uiKeys = Object.keys(ctx.ui).sort();
					expect(toolCall.id).toBe("call-1");
					expect(toolCall.name).toBe("bash");
					expect(toolCall.input.command).toBe("rm -rf tmp");
					return { action: "reject-and-continue", message: "nope" };
				});
			},
		};

		const manager = new PluginManager(
			[plugin],
			createPluginContext([], runtime),
		);
		manager.initialize();

		const decision = await approvalHandler?.({
			toolCallId: "call-1",
			toolName: "bash",
			args: { command: "rm -rf tmp" },
		});
		manager.dispose();

		expect(decision).toEqual({ approved: false, reason: "nope" });
		expect(disposed).toBe(true);
		expect(uiKeys).toEqual(["confirm", "input", "select", "toast"]);
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
