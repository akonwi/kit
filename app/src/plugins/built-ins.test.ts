import { describe, expect, test } from "bun:test";
import { createBuiltInPlugins } from "./built-ins";
import type { PluginContext } from "./types";

describe("createBuiltInPlugins", () => {
	test("loads only headless-safe built-ins in headless mode", () => {
		const plugins = createBuiltInPlugins({ runtime: {} } as PluginContext, {
			headless: true,
		});
		const names = plugins.map((plugin) => plugin.name);

		expect(names).toContain("SkillsPlugin");
		expect(names).toContain("SubagentsPlugin");
		expect(names).toContain("McpPluginWithOptions");
		expect(names).not.toContain("VcsStatusPlugin");
		expect(names).toContain("SessionCwdPlugin");
		expect(names).not.toContain("PromptsPlugin");
		expect(names).not.toContain("PagerPlugin");
		expect(names).not.toContain("GuidedQuestionsPlugin");
		expect(names).not.toContain("UserInteractionToolsPlugin");
		expect(names).not.toContain("NotificationsPlugin");
		expect(names).not.toContain("SessionNamingPlugin");
		expect(names).not.toContain("SettingsPlugin");
	});

	test("adds renderer-neutral chrome for web headless mode", () => {
		const plugins = createBuiltInPlugins({ runtime: {} } as PluginContext, {
			headless: true,
			remoteChrome: true,
		});
		const names = plugins.map((plugin) => plugin.name);

		expect(names).toContain("VcsStatusPlugin");
		expect(names).not.toContain("ReleasesPlugin");
		expect(names).not.toContain("PromptsPlugin");
	});

	test("adds prompt commands for web headless mode", () => {
		const plugins = createBuiltInPlugins({ runtime: {} } as PluginContext, {
			headless: true,
			remotePromptCommands: true,
		});
		const names = plugins.map((plugin) => plugin.name);

		expect(names).toContain("PromptsPlugin");
	});

	test("adds transport-safe interaction tools for remote headless mode", () => {
		const plugins = createBuiltInPlugins({ runtime: {} } as PluginContext, {
			headless: true,
			remoteGuidedQuestions: {
				activate: async () => ({ cancelled: true, answers: {} }),
			},
		});
		const names = plugins.map((plugin) => plugin.name);

		expect(names).toContain("RemoteGuidedQuestionsPlugin");
		expect(names).toContain("RemoteUserInteractionToolsPlugin");
		expect(names).not.toContain("GuidedQuestionsPlugin");
		expect(names).not.toContain("UserInteractionToolsPlugin");
	});
});
