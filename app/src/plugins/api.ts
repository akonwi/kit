import type { Command } from "../features/commands/types";
import { ringBell } from "../features/notifications/notifications";
import type { AgentTool, Static, TSchema } from "../runtime/agent";
import type {
	AgentRuntimeEvent,
	RuntimeEventName,
	RuntimeEventPrefixSubscription,
	ToolApprovalRequest,
} from "../runtime/agent-runtime";
import { saveSettings } from "../settings";
import { openExternal } from "../shell/open-external";
import { resolveAndApplyTheme } from "../shell/theme";
import type {
	CommandContext,
	Disposer,
	EventContext,
	InternalPluginAPI,
	InternalPluginCommandContext,
	InternalPluginEventContext,
	PluginAPI,
	PluginContext,
	ToolCall,
	ToolCallDecision,
	ToolDefinition,
} from "./types";

function toAgentTool<TParameters extends TSchema, TDetails>(
	tool: ToolDefinition<TParameters, TDetails>,
): AgentTool<TParameters, TDetails> {
	const agentTool: AgentTool<TParameters, TDetails> = {
		name: tool.name,
		label: tool.label ?? tool.name,
		description: tool.description,
		parameters: tool.parameters,
		prepareArguments: tool.prepareArguments,
		executionMode: tool.executionMode,
		execute: (
			toolCallId: string,
			params: Static<TParameters>,
			signal?: AbortSignal,
			onUpdate?: Parameters<AgentTool<TParameters, TDetails>["execute"]>[3],
		) => tool.execute(toolCallId, params, signal, onUpdate),
	};
	return Object.assign(agentTool, {
		promptSnippet: tool.promptSnippet,
		promptGuidelines: tool.promptGuidelines,
	});
}

function toPublicPluginUI(
	ctx: PluginContext,
	notifyUserInteraction: () => void,
): PluginAPI["ui"] {
	return {
		text: ctx.ui.text,
		theme: ctx.ui.theme,
		toast: ctx.ui.toast,
		select: ctx.ui.select,
		input: ctx.ui.input,
		confirm: (input) => {
			notifyUserInteraction();
			return ctx.ui.confirm(input);
		},
	};
}

function toRecord(value: unknown): Record<string, unknown> {
	return value && typeof value === "object"
		? (value as Record<string, unknown>)
		: {};
}

function toToolCall(request: ToolApprovalRequest): ToolCall {
	return {
		id: request.toolCallId,
		name: request.toolName,
		input: toRecord(request.args),
	};
}

function toToolApprovalDecision(decision: ToolCallDecision) {
	if (!decision || decision.action === "allow") return true;
	return {
		approved: false,
		reason: decision.message,
	};
}

type CreatePluginAPIBaseOptions = {
	name: string;
	checkContributionConflicts?: boolean;
	addDisposer: (disposer: Disposer) => Disposer;
};

type CreatePublicPluginAPIOptions = CreatePluginAPIBaseOptions & {
	exposeInternalUi?: false;
};

type CreateInternalPluginAPIOptions = CreatePluginAPIBaseOptions & {
	exposeInternalUi: true;
};

export function createPluginAPI(
	ctx: PluginContext,
	options: CreateInternalPluginAPIOptions,
): InternalPluginAPI;
export function createPluginAPI(
	ctx: PluginContext,
	options: CreatePublicPluginAPIOptions,
): PluginAPI;
export function createPluginAPI(
	ctx: PluginContext,
	options: CreatePluginAPIBaseOptions & { exposeInternalUi?: boolean },
): PluginAPI | InternalPluginAPI {
	function track(disposer: Disposer): Disposer {
		let active = true;
		let removeTrackedDisposer: Disposer = () => {};
		const wrapped = () => {
			if (!active) return;
			active = false;
			removeTrackedDisposer();
			disposer();
		};
		removeTrackedDisposer = options.addDisposer(wrapped);
		return wrapped;
	}

	const logger = {
		log: (...args: unknown[]) => {
			console.log(`[plugin:${options.name}]`, ...args);
		},
	};

	const session = {
		get: () => ctx.runtime.getSession(),
		getMessages: () => ctx.runtime.getMessages(),
		setName: (name: string) => ctx.runtime.setSessionName(name),
		submitMessage: (input: Parameters<typeof ctx.runtime.submitMessage>[0]) =>
			ctx.runtime.submitMessage(input),
		submitPromptCommandMessage: (
			command: string,
			args: string,
			expandedPrompt: string,
		) => ctx.runtime.submitPromptCommandMessage(command, args, expandedPrompt),
	};

	const settings = {
		get: () => ctx.settings.settings,
		update: async (patch: Parameters<PluginAPI["settings"]["update"]>[0]) => {
			const previousTheme = ctx.settings.settings.theme ?? "system";
			const next = { ...ctx.settings.settings, ...patch };
			const nextTheme = next.theme ?? "system";
			await saveSettings(next);
			ctx.settings.settings = next;
			if (nextTheme !== previousTheme) {
				await resolveAndApplyTheme(nextTheme);
			}
			ctx.runtime.emitSettingsChanged(next);
		},
	};

	const model = {
		getCurrent: () => ctx.runtime.getCurrentModel(),
	};

	const vcs = {
		get: () => ctx.runtime.vcsInfo,
	};

	function requireChromeItemId(id: string): string {
		const itemId = id.trim();
		if (!itemId) throw new Error("Chrome item id is required.");
		return itemId;
	}

	function namespacedChromeItemId(id: string): string {
		return `${options.name}:${requireChromeItemId(id)}`;
	}

	function createChromeClickHandler(
		id: string,
		onClick: (() => void | Promise<void>) | undefined,
	): (() => Promise<void>) | undefined {
		if (!onClick) return undefined;
		return async () => {
			try {
				await onClick();
			} catch (error) {
				logger.log(`Chrome contribution ${id} click handler failed:`, error);
			}
		};
	}

	function createChromeApi(
		controller: PluginContext["footer"] | PluginContext["header"],
	) {
		return {
			set: (
				id: string,
				content: Parameters<PluginAPI["footer"]["set"]>[1],
				itemOptions?: Parameters<PluginAPI["footer"]["set"]>[2],
			) => {
				const contributionId = namespacedChromeItemId(id);
				controller.setContribution({
					id: contributionId,
					content,
					side: itemOptions?.side,
					onClick: createChromeClickHandler(
						contributionId,
						itemOptions?.onClick,
					),
				});
			},
			clear: (id: string) => {
				controller.clearContribution(namespacedChromeItemId(id));
			},
			hide: (id: string) => {
				return track(controller.hideContribution(requireChromeItemId(id)));
			},
		};
	}

	const footer = createChromeApi(ctx.footer);
	const header = createChromeApi(ctx.header);
	track(() => ctx.footer.clearNamespace(options.name));
	track(() => ctx.header.clearNamespace(options.name));

	const system = {
		get cwd() {
			return ctx.runtime.getSession().cwd;
		},
		open: async (url: string | URL) => {
			await openExternal(url.toString());
		},
		notify: (message: string, title?: string) =>
			ctx.triggerNotification(message, title),
	};

	function notifyUserInteraction(): void {
		ringBell(false, ctx.settings.settings.bells !== false, {
			notify: ctx.triggerNotification,
			title: "Kit",
			message: "Input needed",
		});
	}

	const publicUi = toPublicPluginUI(ctx, notifyUserInteraction);
	const ui = options.exposeInternalUi ? ctx.ui : publicUi;

	function createPublicEventContext(): EventContext {
		return {
			logger,
			ui: publicUi,
			session,
			settings,
			model,
			footer,
			header,
			system,
		} as unknown as EventContext;
	}

	function createInternalEventContext(): InternalPluginEventContext {
		return {
			logger,
			ui: ctx.ui,
			session,
			settings,
			model,
			vcs,
			footer,
			header,
			system,
		};
	}

	function createEventContext(): EventContext | InternalPluginEventContext {
		return options.exposeInternalUi
			? createInternalEventContext()
			: createPublicEventContext();
	}

	function createCommandContext(
		args: string,
	): CommandContext | InternalPluginCommandContext {
		return {
			...createEventContext(),
			args,
		};
	}

	type AnyPluginEventHandler = (
		event: AgentRuntimeEvent,
		ctx: EventContext | InternalPluginEventContext,
	) => void | Promise<void>;

	const on = ((typeOrHandler: unknown, maybeHandler?: unknown) => {
		if (typeof typeOrHandler === "function") {
			const handler = typeOrHandler as AnyPluginEventHandler;
			return track(
				ctx.runtime.subscribe((event) => {
					void handler(event, createEventContext());
				}),
			);
		}

		if (typeof maybeHandler !== "function") {
			throw new Error("kit.on requires an event handler");
		}

		const handler = maybeHandler as AnyPluginEventHandler;
		if (typeof typeOrHandler === "object" && typeOrHandler !== null) {
			const subscription =
				typeOrHandler as RuntimeEventPrefixSubscription<string>;
			return track(
				ctx.runtime.subscribe(subscription, (event) => {
					void handler(event, createEventContext());
				}),
			);
		}

		const type = typeOrHandler as RuntimeEventName;
		return track(
			ctx.runtime.subscribe(type, (event: AgentRuntimeEvent) => {
				void handler(event, createEventContext());
			}),
		);
	}) as PluginAPI["on"] & InternalPluginAPI["on"];

	type AnyCommandHandler = (
		ctx: CommandContext | InternalPluginCommandContext,
	) => void | Promise<void>;

	const registerCommand = ((
		id: string,
		commandOptions: Parameters<PluginAPI["registerCommand"]>[1],
		handler: AnyCommandHandler,
	) => {
		if (
			options.checkContributionConflicts &&
			ctx.commands.getAll().some((command) => command.name === id)
		) {
			throw new Error(`Command /${id} is already registered.`);
		}
		const command: Command = {
			name: id,
			description: commandOptions.description ?? commandOptions.title ?? "",
			argName: commandOptions.argName,
			category: commandOptions.category,
			execute: async (commandCtx) => {
				await handler(createCommandContext(commandCtx.args));
			},
		};
		return track(ctx.commands.register(command));
	}) as PluginAPI["registerCommand"] & InternalPluginAPI["registerCommand"];

	const registerTool: PluginAPI["registerTool"] &
		InternalPluginAPI["registerTool"] = (tool) => {
		if (
			options.checkContributionConflicts &&
			ctx.runtime.getTools().some((candidate) => candidate.name === tool.name)
		) {
			throw new Error(`Tool ${tool.name} is already registered.`);
		}
		return track(ctx.runtime.addTool(toAgentTool(tool)));
	};

	type AnyToolCallHandler = (
		toolCall: ToolCall,
		ctx: EventContext | InternalPluginEventContext,
		signal?: AbortSignal,
	) => ToolCallDecision | Promise<ToolCallDecision>;

	const onToolCall = ((handler: AnyToolCallHandler) =>
		track(
			ctx.runtime.addToolApprovalHandler(async (request, signal) => {
				const decision = await handler(
					toToolCall(request),
					createEventContext(),
					signal,
				);
				return toToolApprovalDecision(decision);
			}),
		)) as PluginAPI["onToolCall"] & InternalPluginAPI["onToolCall"];

	const api = {
		logger,
		ui,
		session,
		settings,
		model,
		footer,
		header,
		system,
		on,
		registerCommand,
		registerTool,
		onToolCall,
		addSystemPrompt: (text: string) =>
			track(ctx.runtime.addSystemPromptAddition(text)),
		addDebugSection: (key: string, lines: string[]) => {
			if (
				options.checkContributionConflicts &&
				ctx.runtime.getDebugSections().has(key)
			) {
				throw new Error(`Debug section ${key} is already registered.`);
			}
			return track(ctx.runtime.setDebugSection(key, lines));
		},
	};

	if (options.exposeInternalUi) {
		return { ...api, vcs } as unknown as InternalPluginAPI;
	}

	return api as unknown as PluginAPI;
}
