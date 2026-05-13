import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { Api, Model, Static, TSchema } from "@mariozechner/pi-ai";
import type { JSX } from "solid-js";
import type { ToastInput } from "../state/toasts";

export type PluginSubscription = () => void;
export type PluginDispose = () => void;

export type PluginOverlaySurfaceProps = {
	zIndex?: number;
};

export type PluginOverlayComponentProps<T> = {
	done: (result: T) => void;
	surfaceProps: PluginOverlaySurfaceProps;
	active: boolean;
};

export interface PluginUI {
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
	custom: <T>(
		component: (props: PluginOverlayComponentProps<T>) => JSX.Element,
	) => Promise<T>;
	getTranscriptViewport: () => { width: number; height: number } | null;
}

export type PluginToolExecutionMode = "sequential" | "parallel";

export type PluginToolResultContentBlock =
	| { type: "text"; text: string }
	| { type: "image"; data: string; mimeType: string };

export type PluginToolResult<TDetails = unknown> = {
	content: PluginToolResultContentBlock[];
	details: TDetails;
	terminate?: boolean;
};

export type PluginToolUpdateCallback<TDetails = unknown> = (
	partialResult: PluginToolResult<TDetails>,
) => void;

export type PluginToolDefinition<
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
		onUpdate?: PluginToolUpdateCallback<TDetails>,
	) => Promise<PluginToolResult<TDetails>>;
	executionMode?: PluginToolExecutionMode;
};

export type PluginLogger = {
	log: (...args: unknown[]) => void;
};

export type PluginMessagePart = {
	type: string;
	[key: string]: unknown;
};

export type PluginSession = {
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

export type PluginSessionAPI = {
	get: () => PluginSession;
	getMessages: () => AgentMessage[];
	setName: (name: string) => Promise<void>;
	submitMessage: (input: string | PluginMessagePart[]) => Promise<void>;
	submitPromptCommandMessage: (
		command: string,
		args: string,
		expandedPrompt: string,
	) => Promise<void>;
};

export type PluginReviewDiffView = "unified" | "split";

export type PluginSettings = {
	theme?: string;
	bells?: boolean;
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
		view?: PluginReviewDiffView;
	};
	retry?: {
		enabled?: boolean;
		maxRetries?: number;
		baseDelayMs?: number;
		maxDelayMs?: number;
	};
	[key: string]: unknown;
};

export type PluginSettingsAPI = {
	get: () => PluginSettings;
	update: (patch: Partial<PluginSettings>) => Promise<void>;
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

export type PluginRuntimeEvent<Type extends string = string> = {
	type: Type;
} & Record<string, unknown>;

export type PluginEventHandler<Type extends string = string> = (
	event: PluginRuntimeEvent<Type>,
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
	on<Type extends string>(
		type: Type,
		handler: PluginEventHandler<Type>,
	): PluginSubscription;
	on<Prefix extends string>(
		options: { prefix: Prefix },
		handler: PluginEventHandler<`${Prefix}${string}`>,
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
