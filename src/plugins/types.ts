import type { AgentMessage } from "@earendil-works/pi-agent-core";
import type { Api, Model, TSchema } from "@earendil-works/pi-ai";
import type { JSX } from "solid-js";
import type { CommandRegistry } from "../features/commands";
import type { MessagePart as KitMessagePart } from "../messages/parts";
import type {
	AgentRuntime,
	AgentRuntimeEvent,
	RuntimeEventName,
	RuntimeEventNameMatchingPrefix,
	RuntimeEventPrefixSubscription,
} from "../runtime/agent-runtime";
import type { VcsInfo } from "../runtime/vcs-info";
import type { Session as KitSession } from "../session";
import type { Settings as KitSettings, LoadedSettings } from "../settings";
import type { AttachmentsController } from "../shell/attachments-controller";
import type { FooterStatusController } from "../shell/footer-status";
import type { HeaderStatusController } from "../shell/header-status";
import type {
	CommandOptions,
	Disposer,
	PluginAPI,
	ToolCall,
	ToolCallDecision,
	ToolDefinition,
} from "./sdk";

export type {
	ChromeContributionOptions,
	ChromeContributionSide,
	CommandContext,
	CommandOptions,
	Disposer,
	EventContext,
	EventHandler,
	KitText,
	KitTextContent,
	KitTextStyle,
	MessagePart,
	Plugin,
	PluginAPI,
	RuntimeEvent,
	Session,
	Settings,
	SyntaxPalette,
	ThemeConfig,
	ThemeTokens,
	ToolCall,
	ToolCallDecision,
	ToolCallHandler,
	ToolDefinition,
	ToolExecutionMode,
	ToolResult,
	ToolResultContent,
	ToolUpdateCallback,
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

export type InternalPluginUI = PluginAPI["ui"] & {
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
	footer: FooterStatusController;
	header: HeaderStatusController;
};

export type InternalPluginSessionAPI = {
	get: () => KitSession;
	getMessages: () => AgentMessage[];
	setName: (name: string) => Promise<void>;
	submitMessage: (input: string | KitMessagePart[]) => Promise<void>;
	submitPromptCommandMessage: (
		command: string,
		args: string,
		expandedPrompt: string,
	) => Promise<void>;
};

export type InternalPluginSettingsAPI = {
	get: () => KitSettings;
	update: (patch: Partial<KitSettings>) => Promise<void>;
};

export type InternalPluginModelAPI = {
	getCurrent: () => Model<Api> | undefined;
};

export type InternalPluginVcsAPI = {
	get: () => VcsInfo;
};

export type InternalPluginFooterAPI = PluginAPI["footer"];
export type InternalPluginHeaderAPI = PluginAPI["header"];

export type InternalPluginSystemAPI = {
	readonly cwd: string;
	open: (url: string | URL) => Promise<void>;
};

export type InternalPluginEventContext = {
	logger: PluginAPI["logger"];
	ui: InternalPluginUI;
	session: InternalPluginSessionAPI;
	settings: InternalPluginSettingsAPI;
	model: InternalPluginModelAPI;
	vcs: InternalPluginVcsAPI;
	footer: InternalPluginFooterAPI;
	header: InternalPluginHeaderAPI;
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
	logger: PluginAPI["logger"];
	ui: InternalPluginUI;
	session: InternalPluginSessionAPI;
	settings: InternalPluginSettingsAPI;
	model: InternalPluginModelAPI;
	vcs: InternalPluginVcsAPI;
	footer: InternalPluginFooterAPI;
	header: InternalPluginHeaderAPI;
	system: InternalPluginSystemAPI;
	on(handler: InternalPluginEventHandler): Disposer;
	on<K extends RuntimeEventName>(
		type: K,
		handler: InternalPluginEventHandler<K>,
	): Disposer;
	on<P extends string>(
		options: RuntimeEventPrefixSubscription<P>,
		handler: InternalPluginEventHandler<RuntimeEventNameMatchingPrefix<P>>,
	): Disposer;
	registerCommand: (
		id: string,
		options: CommandOptions,
		handler: (ctx: InternalPluginCommandContext) => void | Promise<void>,
	) => Disposer;
	registerTool: <TParameters extends TSchema, TDetails>(
		tool: ToolDefinition<TParameters, TDetails>,
	) => Disposer;
	onToolCall: (handler: InternalToolCallHandler) => Disposer;
	addSystemPrompt: (text: string) => Disposer;
	addDebugSection: (key: string, lines: string[]) => Disposer;
}

export type InternalPluginDefinition = (
	kit: InternalPluginAPI,
	// biome-ignore lint/suspicious/noConfusingVoidType: plugin definitions may omit a return value or return a disposer.
) => void | Disposer;
