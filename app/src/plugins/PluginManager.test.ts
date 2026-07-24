import { describe, expect, test } from "bun:test";
import type { Command } from "../features/commands";
import type { ToolApprovalHandler } from "../runtime/agent-runtime";
import { createChromeContributionsController } from "../shell/chrome-contributions";
import { buildDefaultTheme } from "../shell/themes/system";
import { PluginManager, type PluginRegistration } from "./PluginManager";
import type { PluginAPI, PluginContext, ThemeConfig } from "./types";

function createThemeConfig(name = "test"): ThemeConfig {
	const theme = buildDefaultTheme();
	return {
		name,
		tokens: { ...theme.tokens },
		syntaxPalette: { ...theme.syntaxPalette },
	};
}

function createPluginContext(
	commands: Command[],
	runtime: Partial<PluginContext["runtime"]> = {},
): PluginContext {
	const themeConfig = createThemeConfig();
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
			subscribe: () => () => {},
		},
		settings: { settings: {}, paths: {} as PluginContext["settings"]["paths"] },
		ui: {
			text: (text, style) => ({ __kitText: true, text, style }),
			theme: () => themeConfig,
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
			isHidden: () => false,
			getContributions: () => [],
			subscribe: () => () => {},
		},
		header: {
			setContribution: () => {},
			clearContribution: () => {},
			clearNamespace: () => {},
			hideContribution: () => () => {},
			isHidden: () => false,
			getContributions: () => [],
			subscribe: () => () => {},
		},
		triggerNotification: () => false,
	};
}

describe("PluginManager", () => {
	test("awaits asynchronous plugin cleanup in reverse order", async () => {
		const disposed: string[] = [];
		const manager = new PluginManager(
			[
				{
					name: "first",
					initialize: () => async () => {
						disposed.push("first:start");
						await Promise.resolve();
						disposed.push("first:end");
					},
				},
				{
					name: "second",
					initialize: () => async () => {
						disposed.push("second:start");
						await Promise.resolve();
						disposed.push("second:end");
					},
				},
			],
			createPluginContext([]),
		);
		manager.initialize();
		await manager.disposeAsync();
		expect(disposed).toEqual([
			"second:start",
			"second:end",
			"first:start",
			"first:end",
		]);
	});

	test("continues cleanup when a tracked disposer throws", () => {
		const disposed: string[] = [];
		const context = createPluginContext([]);
		context.commands.register = (command) => () => {
			disposed.push(command.name);
			if (command.name === "bad-cleanup") throw new Error("cleanup failed");
		};
		const manager = new PluginManager(
			[
				{
					name: "tracked",
					initialize: (kit: PluginAPI) => {
						kit.registerCommand(
							"good-cleanup",
							{ description: "Good" },
							() => {},
						);
						kit.registerCommand(
							"bad-cleanup",
							{ description: "Bad" },
							() => {},
						);
						return () => disposed.push("returned");
					},
				},
			],
			context,
		);
		const originalError = console.error;
		console.error = () => {};
		try {
			manager.initialize();
			manager.dispose();
		} finally {
			console.error = originalError;
		}
		expect(disposed).toEqual(["bad-cleanup", "good-cleanup", "returned"]);
	});

	test("continues cleanup when a plugin disposer throws", () => {
		const disposed: string[] = [];
		const manager = new PluginManager(
			[
				{
					name: "first",
					initialize: () => () => {
						disposed.push("first");
					},
				},
				{
					name: "second",
					initialize: () => () => {
						disposed.push("second");
						throw new Error("cleanup failed");
					},
				},
			],
			createPluginContext([]),
		);
		const originalError = console.error;
		console.error = () => {};
		try {
			manager.initialize();
			manager.dispose();
		} finally {
			console.error = originalError;
		}
		expect(disposed).toEqual(["second", "first"]);
	});

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

		expect(uiKeys).toEqual([
			"confirm",
			"input",
			"select",
			"text",
			"theme",
			"toast",
		]);
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
			"text",
			"theme",
			"toast",
		]);
	});

	test("exposes theme config through public ui", () => {
		const expectedTheme = createThemeConfig("plugin-test");
		let receivedTheme: ThemeConfig | undefined;
		const plugin: PluginRegistration = {
			name: "external:theme",
			initialize: (kit) => {
				receivedTheme = kit.ui.theme();
			},
		};
		const context = createPluginContext([]);
		context.ui.theme = () => expectedTheme;

		const manager = new PluginManager([plugin], context);
		manager.initialize();

		expect(receivedTheme).toEqual(expectedTheme);
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
		expect(uiKeys).toEqual([
			"confirm",
			"input",
			"select",
			"text",
			"theme",
			"toast",
		]);
	});

	test("supports styled clickable chrome contributions", async () => {
		const footer = createChromeContributionsController();
		let clicked = false;
		const plugin: PluginRegistration = {
			name: "external:chrome",
			initialize: (kit) => {
				kit.footer.set(
					"ci",
					[kit.ui.text("✓", { fg: "green", bold: true }), " passing"],
					{
						side: "right",
						onClick: () => {
							clicked = true;
						},
					},
				);
			},
		};
		const context = createPluginContext([]);
		context.footer = footer;

		const manager = new PluginManager([plugin], context);
		manager.initialize();

		const [contribution] = footer.getContributions();
		expect(contribution.id).toBe("external:chrome:ci");
		expect(contribution.side).toBe("right");
		expect(contribution.content).toEqual([
			{ text: "✓", style: { fg: "green", bold: true } },
			{ text: " passing" },
		]);

		await contribution.onClick?.();
		expect(clicked).toBe(true);
	});

	test("logs chrome contribution click handler errors", async () => {
		const footer = createChromeContributionsController();
		const logs: unknown[][] = [];
		const originalLog = console.log;
		console.log = (...args: unknown[]) => {
			logs.push(args);
		};
		try {
			const plugin: PluginRegistration = {
				name: "external:chrome",
				initialize: (kit) => {
					kit.footer.set("bad", "bad", {
						onClick: () => {
							throw new Error("nope");
						},
					});
				},
			};
			const context = createPluginContext([]);
			context.footer = footer;

			const manager = new PluginManager([plugin], context);
			manager.initialize();

			await footer.getContributions()[0].onClick?.();
		} finally {
			console.log = originalLog;
		}

		expect(String(logs[0]?.[1])).toContain(
			"Chrome contribution external:chrome:bad click handler failed",
		);
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
