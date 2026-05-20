import type { PluginAPI, ToolCall } from "@akonwi/kit/plugin";

const RISKY_BASH_PATTERNS = [/\bgit\s+commit\b/, /\bnpm\s+publish\b/];

export default function ToolApprovalPlugin(kit: PluginAPI) {
	kit.onToolCall(async (toolCall, ctx) => {
		if (!needsApproval(toolCall)) return { action: "allow" };

		const approved = await ctx.ui.confirm({
			title: `Allow ${toolCall.name}?`,
			message: summarizeCall(toolCall),
			confirmLabel: "Allow",
			cancelLabel: "Block",
			defaultValue: false,
		});

		if (approved) return { action: "allow" };
		return {
			action: "reject-and-continue",
			message: `The user rejected ${toolCall.name}.`,
		};
	});
}

function needsApproval(toolCall: ToolCall): boolean {
	if (toolCall.name !== "bash") return false;
	const command = getStringArg(toolCall.input, "command");
	return RISKY_BASH_PATTERNS.some((pattern) => pattern.test(command));
}

function summarizeCall(toolCall: ToolCall): string {
	if (toolCall.name === "bash") {
		return truncate(getStringArg(toolCall.input, "command") || "(no command)");
	}
	const path = getStringArg(toolCall.input, "path");
	if (path) return truncate(path);
	try {
		return truncate(JSON.stringify(toolCall.input));
	} catch {
		return "Unable to summarize tool arguments.";
	}
}

function getStringArg(args: Record<string, unknown>, key: string): string {
	const value = args[key];
	return typeof value === "string" ? value : "";
}

function truncate(value: string): string {
	return value.length > 140 ? `${value.slice(0, 137)}...` : value;
}
