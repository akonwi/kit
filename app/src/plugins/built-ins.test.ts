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
});
