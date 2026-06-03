import type { InternalPluginAPI, ToolDefinition } from "../../plugins";
import { Type } from "../../runtime/agent";

const changeCwdParameters = Type.Object({
	path: Type.String({
		description:
			"Directory to make the current working directory for this session. Relative paths resolve from the session cwd.",
	}),
});

type ChangeCwdDetails = {
	previousCwd: string;
	cwd: string;
	changed: boolean;
};

function createChangeCwdTool(
	kit: InternalPluginAPI,
): ToolDefinition<typeof changeCwdParameters, ChangeCwdDetails> {
	return {
		name: "change_cwd",
		description:
			"Change the current working directory for this Kit session. Use when the work should move to a different directory.",
		parameters: changeCwdParameters,
		execute: async (_toolCallId, params) => {
			const previousCwd = kit.session.get().cwd;
			const session = await kit.session.changeCwd(params.path, "agent");
			const changed = previousCwd !== session.cwd;
			return {
				content: [
					{
						type: "text" as const,
						text: changed
							? `Changed session cwd to ${session.cwd}`
							: `Already in directory: ${session.cwd}`,
					},
				],
				details: {
					previousCwd,
					cwd: session.cwd,
					changed,
				},
			};
		},
	};
}

export function SessionCwdPlugin(kit: InternalPluginAPI): void {
	kit.registerCommand(
		"cd",
		{
			description: "Change the current working directory for this session",
			argName: "path",
		},
		async (ctx) => {
			const target = ctx.args.trim();
			if (!target) {
				ctx.ui.toast({
					title: "Usage: /cd <path>",
					variant: "warning",
				});
				return;
			}
			try {
				const previousCwd = ctx.session.get().cwd;
				const session = await ctx.session.changeCwd(target, "user");
				ctx.ui.toast({
					title:
						previousCwd === session.cwd
							? "Already in directory"
							: "Changed working directory",
					subtitle: session.cwd,
					variant: "info",
				});
			} catch (error) {
				ctx.ui.toast({
					title: "Failed to change directory",
					subtitle: error instanceof Error ? error.message : String(error),
					variant: "error",
				});
			}
		},
	);

	kit.registerTool(createChangeCwdTool(kit));
}
