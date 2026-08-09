import { describe, expect, test } from "bun:test";
import type {
	InternalPluginAPI,
	InternalPluginCommandOptions,
} from "../../plugins";
import { SessionCwdPlugin } from ".";

describe("SessionCwdPlugin", () => {
	test("opts the cd command into transport-neutral execution", async () => {
		let commandOptions: InternalPluginCommandOptions | undefined;
		const changes: Array<[string, "user" | "agent" | undefined]> = [];
		const kit = {
			registerCommand: (_id: string, options: InternalPluginCommandOptions) => {
				commandOptions = options;
				return () => {};
			},
			registerTool: () => () => {},
			session: {
				changeCwd: async (path: string, source?: "user" | "agent") => {
					changes.push([path, source]);
					return { cwd: path };
				},
			},
		} as unknown as InternalPluginAPI;
		SessionCwdPlugin(kit);
		const execute = commandOptions?.executeTransportNeutral;
		if (!execute) throw new Error("Expected transport-neutral cd command");

		await execute({ args: "  /tmp/project  " });
		expect(changes).toEqual([["/tmp/project", "user"]]);
		await expect(execute({ args: " " })).rejects.toThrow(
			"Working directory path is required",
		);
	});
});
