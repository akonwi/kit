export const RPC_PROTOCOL_VERSION = 2;

export const RPC_BASE_COMMAND_TYPES = [
	"prompt",
	"steer",
	"follow_up",
	"restore_follow_ups",
	"promote_follow_ups",
	"acknowledge_follow_up_mutation",
	"abort",
	"new_session",
	"list_sessions",
	"open_session",
	"change_cwd",
	"get_capabilities",
	"get_state",
	"get_messages",
	"get_last_assistant_text",
	"get_available_models",
	"set_model",
	"get_available_thinking_levels",
	"set_thinking_level",
	"get_scratchpad",
	"update_scratchpad",
] as const;

export type RpcBaseCommandType = (typeof RPC_BASE_COMMAND_TYPES)[number];

export type RpcCommand = {
	id?: unknown;
	type: string;
	[key: string]: unknown;
};

export type RpcResponse = {
	id?: unknown;
	type: "response";
	command: string;
	success: boolean;
	data?: unknown;
	error?: string;
};

export type RpcChromeTextStyle = {
	fgToken?: string;
	bgToken?: string;
	bold?: boolean;
	dim?: boolean;
	italic?: boolean;
	underline?: boolean;
	strikethrough?: boolean;
};

export type RpcChromeSegment = {
	text: string;
	style?: RpcChromeTextStyle;
};

export type RpcChromeContribution = {
	id: string;
	content: RpcChromeSegment[];
	plainText: string;
	side: "left" | "right";
	action?: { type: "open-url"; url: string };
	clickable: boolean;
};

export type RpcChromeAreaSnapshot = {
	contributions: RpcChromeContribution[];
	hiddenBuiltinIds: string[];
};

export type RpcChromeSnapshot = {
	header: RpcChromeAreaSnapshot;
	footer: RpcChromeAreaSnapshot;
};

export type RpcConnectionSnapshot = {
	state: Record<string, unknown>;
	messages: unknown[];
	chrome?: RpcChromeSnapshot;
	messageOffset: number;
	totalMessageCount: number;
	pendingInteractions: unknown[];
	pendingInteractionGeneration: number;
};

export function isRpcCommand(value: unknown): value is RpcCommand {
	return (
		typeof value === "object" &&
		value !== null &&
		!Array.isArray(value) &&
		typeof (value as Record<string, unknown>).type === "string"
	);
}

export function parseRpcCommand(value: unknown): RpcCommand {
	if (!isRpcCommand(value)) {
		throw new Error("RPC command must be an object with a string type");
	}
	return value;
}
