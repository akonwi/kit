import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { Api, Model, TSchema } from "@mariozechner/pi-ai";
import type { CommandRegistry } from "../features/commands";
import type { MessagePart } from "../messages/parts";
import type {
	AgentRuntime,
	AgentRuntimeEvent,
	RuntimeEventName,
	RuntimeEventNameMatchingPrefix,
	RuntimeEventPrefixSubscription,
} from "../runtime/agent-runtime";
import type { Session } from "../session";
import type { LoadedSettings, Settings } from "../settings";
import type { AttachmentsController } from "../shell/attachments-controller";
import type {
	PluginDispose,
	PluginLogger,
	PluginSubscription,
	PluginToolDefinition,
	PluginUI,
} from "./sdk";

export type {
	PluginDispose,
	PluginLogger,
	PluginMessagePart,
	PluginOverlayComponentProps,
	PluginOverlaySurfaceProps,
	PluginReviewDiffView,
	PluginRuntimeEvent,
	PluginSession,
	PluginSubscription,
	PluginToolDefinition,
	PluginToolExecutionMode,
	PluginToolResult,
	PluginToolResultContentBlock,
	PluginToolUpdateCallback,
	PluginUI,
} from "./sdk";

export type TranscriptViewport = NonNullable<
	ReturnType<PluginUI["getTranscriptViewport"]>
>;

export type PluginContext = {
	runtime: AgentRuntime;
	commands: CommandRegistry;
	settings: LoadedSettings;
	ui: PluginUI;
	attachments: AttachmentsController;
};

export type PluginSessionAPI = {
	get: () => Session;
	getMessages: () => AgentMessage[];
	setName: (name: string) => Promise<void>;
	submitMessage: (input: string | MessagePart[]) => Promise<void>;
	submitPromptCommandMessage: (
		command: string,
		args: string,
		expandedPrompt: string,
	) => Promise<void>;
};

export type PluginSettingsAPI = {
	get: () => Settings;
	update: (patch: Partial<Settings>) => Promise<void>;
};

export type PluginModelAPI = {
	getCurrent: () => Model<Api> | undefined;
};

export type PluginSystemAPI = {
	readonly cwd: string;
	open: (url: string | URL) => Promise<void>;
};

export type PluginEventContext = {
	logger: PluginLogger;
	ui: PluginUI;
	session: PluginSessionAPI;
	settings: PluginSettingsAPI;
	model: PluginModelAPI;
	system: PluginSystemAPI;
};

export type PluginCommandContext = PluginEventContext & {
	args: string;
};

export type PluginCommandOptions = {
	title?: string;
	description?: string;
	argName?: string;
	category?: string;
};

export type PluginEventHandler<K extends RuntimeEventName = RuntimeEventName> =
	(
		event: AgentRuntimeEvent<K>,
		ctx: PluginEventContext,
	) => void | Promise<void>;

export interface PluginAPI {
	logger: PluginLogger;
	ui: PluginUI;
	session: PluginSessionAPI;
	settings: PluginSettingsAPI;
	model: PluginModelAPI;
	system: PluginSystemAPI;
	on(handler: PluginEventHandler): PluginSubscription;
	on<K extends RuntimeEventName>(
		type: K,
		handler: PluginEventHandler<K>,
	): PluginSubscription;
	on<P extends string>(
		options: RuntimeEventPrefixSubscription<P>,
		handler: PluginEventHandler<RuntimeEventNameMatchingPrefix<P>>,
	): PluginSubscription;
	registerCommand: (
		id: string,
		options: PluginCommandOptions,
		handler: (ctx: PluginCommandContext) => void | Promise<void>,
	) => PluginSubscription;
	registerTool: <TParameters extends TSchema, TDetails>(
		tool: PluginToolDefinition<TParameters, TDetails>,
	) => PluginSubscription;
	addSystemPrompt: (text: string) => PluginSubscription;
	addDebugSection: (key: string, lines: string[]) => PluginSubscription;
}

// biome-ignore lint/suspicious/noConfusingVoidType: plugin definitions may omit a return value or return a disposer.
export type PluginDefinition = (kit: PluginAPI) => void | PluginDispose;
