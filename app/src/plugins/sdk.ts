export { Type } from "typebox";

import type {
	AgentMessage,
	Api,
	Model,
	Static,
	TSchema,
} from "../runtime/agent";
import type {
	SyntaxPalette,
	ThemeConfig,
	ThemeColorTokens as ThemeTokens,
} from "../shell/themes/types";
import type { ToastInput } from "../state/toasts";

export type { SyntaxPalette, ThemeConfig, ThemeTokens };

export type Disposer = () => void;

export type KitTextStyle = {
	fg?: string;
	bg?: string;
	bold?: boolean;
	dim?: boolean;
	italic?: boolean;
	underline?: boolean;
	strikethrough?: boolean;
};

export type KitText = {
	readonly __kitText: true;
	readonly text: string;
	readonly style?: KitTextStyle;
};

export type KitTextContent = string | KitText | readonly (string | KitText)[];

interface UI {
	text: (text: string, style?: KitTextStyle) => KitText;
	theme: () => ThemeConfig;
	toast: (toast: ToastInput) => void;
	select(input: {
		title: string;
		message?: string;
		options: string[];
		filterable?: boolean;
		placeholder?: string;
	}): Promise<string | undefined>;
	select<T>(input: {
		title: string;
		message?: string;
		options: Array<{ label: string; value: T; description?: string }>;
		filterable?: boolean;
		placeholder?: string;
	}): Promise<T | undefined>;
	input(input: {
		title: string;
		message?: string;
		placeholder?: string;
		initialValue?: string;
	}): Promise<string | undefined>;
	confirm(input: {
		title: string;
		message?: string;
		confirmLabel?: string;
		cancelLabel?: string;
		defaultValue?: boolean;
	}): Promise<boolean>;
}

export type ToolExecutionMode = "sequential" | "parallel";

export type ToolCall = {
	id: string;
	name: string;
	input: Record<string, unknown>;
};

export type ToolCallDecision =
	| { action: "allow" }
	| { action: "reject-and-continue"; message?: string }
	| undefined
	// biome-ignore lint/suspicious/noConfusingVoidType: tool call handlers may omit a return value to allow the call.
	| void;

export type ToolCallHandler = (
	toolCall: ToolCall,
	ctx: EventContext,
	signal?: AbortSignal,
) => ToolCallDecision | Promise<ToolCallDecision>;

export type ToolResultContent =
	| { type: "text"; text: string }
	| { type: "image"; data: string; mimeType: string };

export type ToolResult<TDetails = unknown> = {
	content: ToolResultContent[];
	details: TDetails;
	terminate?: boolean;
};

export type ToolUpdateCallback<TDetails = unknown> = (
	partialResult: ToolResult<TDetails>,
) => void;

export type ToolDefinition<
	TParameters extends TSchema = TSchema,
	TDetails = unknown,
> = {
	name: string;
	label?: string;
	description: string;
	promptSnippet?: string;
	promptGuidelines?: string[];
	parameters: TParameters;
	prepareArguments?: (args: unknown) => Static<TParameters>;
	execute: (
		toolCallId: string,
		params: Static<TParameters>,
		signal?: AbortSignal,
		onUpdate?: ToolUpdateCallback<TDetails>,
	) => Promise<ToolResult<TDetails>>;
	executionMode?: ToolExecutionMode;
};

type Logger = {
	log: (...args: unknown[]) => void;
};

export type MessagePart = {
	type: string;
	[key: string]: unknown;
};

export type Session = {
	id: string;
	cwd: string;
	name?: string;
	model?: string;
	thinkingLevel?: string;
	parentSessionId?: string;
	forkedFromTurnId?: string;
	createdAt?: string;
	updatedAt?: string;
	turns?: unknown[];
	[key: string]: unknown;
};

type SessionAPI = {
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

export type KeybindingValue = string | string[] | false | null;
export type KeybindingSettings = Record<string, KeybindingValue>;

export type Settings = {
	theme?: string;
	keybindings?: KeybindingSettings;
	zen?: boolean;
	speech?:
		| boolean
		| {
				enabled?: boolean;
				maxChars?: number;
				voice?: string;
		  };
	pager?: boolean;
	guidedQuestions?: boolean;
	sessionNaming?: boolean;
	diffs?: {
		view?: "unified" | "split";
	};
	retry?: {
		enabled?: boolean;
		maxRetries?: number;
		baseDelayMs?: number;
		maxDelayMs?: number;
	};
	[key: string]: unknown;
};

type SettingsAPI = {
	get: () => Settings;
	update: (patch: Partial<Settings>) => Promise<void>;
};

type ModelAPI = {
	getCurrent: () => Model<Api> | undefined;
};

export type ChromeContributionSide = "left" | "right";

export type ChromeContributionOptions = {
	side?: ChromeContributionSide;
	onClick?: () => void | Promise<void>;
};

type ChromeContributionAPI = {
	set: (
		id: string,
		content: KitTextContent,
		options?: ChromeContributionOptions,
	) => void;
	clear: (id: string) => void;
	hide: (id: string) => Disposer;
};

type FooterAPI = ChromeContributionAPI;
type HeaderAPI = ChromeContributionAPI;

type SystemAPI = {
	readonly cwd: string;
	open: (url: string | URL) => Promise<void>;
};

export type EventContext = {
	logger: Logger;
	ui: UI;
	session: SessionAPI;
	settings: SettingsAPI;
	model: ModelAPI;
	footer: FooterAPI;
	header: HeaderAPI;
	system: SystemAPI;
};

export type CommandContext = EventContext & {
	args: string;
};

export type CommandOptions = {
	title?: string;
	description?: string;
	argName?: string;
	category?: string;
};

export type RuntimeEvent<Type extends string = string> = {
	type: Type;
} & Record<string, unknown>;

export type EventHandler<Type extends string = string> = (
	event: RuntimeEvent<Type>,
	ctx: EventContext,
) => void | Promise<void>;

export interface PluginAPI {
	logger: Logger;
	ui: UI;
	session: SessionAPI;
	settings: SettingsAPI;
	model: ModelAPI;
	footer: FooterAPI;
	header: HeaderAPI;
	system: SystemAPI;
	on(handler: EventHandler): Disposer;
	on<Type extends string>(type: Type, handler: EventHandler<Type>): Disposer;
	on<Prefix extends string>(
		options: { prefix: Prefix },
		handler: EventHandler<`${Prefix}${string}`>,
	): Disposer;
	registerCommand: (
		id: string,
		options: CommandOptions,
		handler: (ctx: CommandContext) => void | Promise<void>,
	) => Disposer;
	registerTool: <TParameters extends TSchema, TDetails>(
		tool: ToolDefinition<TParameters, TDetails>,
	) => Disposer;
	registerSubagent: (def: {
		name: string;
		description: string;
		model?: string;
		instructions: string;
	}) => Disposer;
	onToolCall: (handler: ToolCallHandler) => Disposer;
	addSystemPrompt: (text: string) => Disposer;
	addDebugSection: (key: string, lines: string[]) => Disposer;
}

export type Plugin = (
	kit: PluginAPI,
	// biome-ignore lint/suspicious/noConfusingVoidType: plugin definitions may omit a return value or return a disposer.
) => void | Disposer;
