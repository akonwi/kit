import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { Api, Model, TSchema } from "@mariozechner/pi-ai";
import type { JSX } from "solid-js";
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
	PluginCommandOptions,
	PluginDispose,
	PluginLogger,
	PluginSubscription,
	PluginToolDefinition,
	PluginUI,
	ToolCall,
	ToolCallDecision,
} from "./sdk";

export type {
	PluginAPI,
	PluginCommandContext,
	PluginCommandOptions,
	PluginDefinition,
	PluginDispose,
	PluginEventContext,
	PluginEventHandler,
	PluginLogger,
	PluginMessagePart,
	PluginModelAPI,
	PluginReviewDiffView,
	PluginRuntimeEvent,
	PluginSession,
	PluginSessionAPI,
	PluginSettings,
	PluginSettingsAPI,
	PluginSubscription,
	PluginSystemAPI,
	PluginToolDefinition,
	PluginToolExecutionMode,
	PluginToolResult,
	PluginToolResultContentBlock,
	PluginToolUpdateCallback,
	PluginUI,
	ToolCall,
	ToolCallDecision,
	ToolCallHandler,
} from "./sdk";

export type TranscriptViewport = { width: number; height: number };

export type InternalPluginOverlaySurfaceProps = {
	zIndex?: number;
};

export type InternalPluginOverlayComponentProps<T> = {
	done: (result: T) => void;
	surfaceProps: InternalPluginOverlaySurfaceProps;
	active: boolean;
};

export type InternalPluginUI = PluginUI & {
	custom: <T>(
		component: (props: InternalPluginOverlayComponentProps<T>) => JSX.Element,
	) => Promise<T>;
	getTranscriptViewport: () => TranscriptViewport | null;
};

export type PluginContext = {
	runtime: AgentRuntime;
	commands: CommandRegistry;
	settings: LoadedSettings;
	ui: InternalPluginUI;
	attachments: AttachmentsController;
};

export type InternalPluginSessionAPI = {
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

export type InternalPluginSettingsAPI = {
	get: () => Settings;
	update: (patch: Partial<Settings>) => Promise<void>;
};

export type InternalPluginModelAPI = {
	getCurrent: () => Model<Api> | undefined;
};

export type InternalPluginSystemAPI = {
	readonly cwd: string;
	open: (url: string | URL) => Promise<void>;
};

export type InternalPluginEventContext = {
	logger: PluginLogger;
	ui: InternalPluginUI;
	session: InternalPluginSessionAPI;
	settings: InternalPluginSettingsAPI;
	model: InternalPluginModelAPI;
	system: InternalPluginSystemAPI;
};

export type InternalPluginCommandContext = InternalPluginEventContext & {
	args: string;
};

export type InternalPluginEventHandler<
	K extends RuntimeEventName = RuntimeEventName,
> = (
	event: AgentRuntimeEvent<K>,
	ctx: InternalPluginEventContext,
) => void | Promise<void>;

export type InternalToolCallHandler = (
	toolCall: ToolCall,
	ctx: InternalPluginEventContext,
	signal?: AbortSignal,
) => ToolCallDecision | Promise<ToolCallDecision>;

export interface InternalPluginAPI {
	logger: PluginLogger;
	ui: InternalPluginUI;
	session: InternalPluginSessionAPI;
	settings: InternalPluginSettingsAPI;
	model: InternalPluginModelAPI;
	system: InternalPluginSystemAPI;
	on(handler: InternalPluginEventHandler): PluginSubscription;
	on<K extends RuntimeEventName>(
		type: K,
		handler: InternalPluginEventHandler<K>,
	): PluginSubscription;
	on<P extends string>(
		options: RuntimeEventPrefixSubscription<P>,
		handler: InternalPluginEventHandler<RuntimeEventNameMatchingPrefix<P>>,
	): PluginSubscription;
	registerCommand: (
		id: string,
		options: PluginCommandOptions,
		handler: (ctx: InternalPluginCommandContext) => void | Promise<void>,
	) => PluginSubscription;
	registerTool: <TParameters extends TSchema, TDetails>(
		tool: PluginToolDefinition<TParameters, TDetails>,
	) => PluginSubscription;
	onToolCall: (handler: InternalToolCallHandler) => PluginSubscription;
	addSystemPrompt: (text: string) => PluginSubscription;
	addDebugSection: (key: string, lines: string[]) => PluginSubscription;
}

export type InternalPluginDefinition = (
	kit: InternalPluginAPI,
	// biome-ignore lint/suspicious/noConfusingVoidType: plugin definitions may omit a return value or return a disposer.
) => void | PluginDispose;
