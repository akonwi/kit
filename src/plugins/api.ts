import type { AgentTool } from "@mariozechner/pi-agent-core";
import type { Static, TSchema } from "@mariozechner/pi-ai";
import type { Command } from "../features/commands/types";
import type {
	AgentRuntimeEvent,
	RuntimeEventName,
	RuntimeEventPrefixSubscription,
} from "../runtime/agent-runtime";
import { saveSettings } from "../settings";
import { openExternal } from "../shell/open-external";
import type {
	PluginAPI,
	PluginCommandContext,
	PluginContext,
	PluginDispose,
	PluginEventContext,
	PluginEventHandler,
	PluginSubscription,
	PluginToolDefinition,
} from "./types";

function toAgentTool<TParameters extends TSchema, TDetails>(
	tool: PluginToolDefinition<TParameters, TDetails>,
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

export function createPluginAPI(
	ctx: PluginContext,
	options: {
		name: string;
		addDisposer: (disposer: PluginDispose) => PluginDispose;
	},
): PluginAPI {
	function track(disposer: PluginDispose): PluginSubscription {
		let active = true;
		let removeTrackedDisposer: PluginDispose = () => {};
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
			const next = { ...ctx.settings.settings, ...patch };
			await saveSettings(next);
			ctx.settings.settings = next;
			ctx.runtime.emitSettingsChanged(next);
		},
	};

	const model = {
		getCurrent: () => ctx.runtime.getCurrentModel(),
	};

	const system = {
		get cwd() {
			return ctx.runtime.getSession().cwd;
		},
		open: async (url: string | URL) => {
			await openExternal(url.toString());
		},
	};

	function createEventContext(): PluginEventContext {
		return {
			logger,
			ui: ctx.ui,
			session,
			settings,
			model,
			system,
		};
	}

	function createCommandContext(args: string): PluginCommandContext {
		return {
			...createEventContext(),
			args,
		};
	}

	const on = ((typeOrHandler: unknown, maybeHandler?: unknown) => {
		if (typeof typeOrHandler === "function") {
			const handler = typeOrHandler as PluginEventHandler;
			return track(
				ctx.runtime.subscribe((event) => {
					void handler(event, createEventContext());
				}),
			);
		}

		if (typeof maybeHandler !== "function") {
			throw new Error("kit.on requires an event handler");
		}

		const handler = maybeHandler as PluginEventHandler;
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
	}) as PluginAPI["on"];

	const registerCommand: PluginAPI["registerCommand"] = (
		id,
		commandOptions,
		handler,
	) => {
		const command: Command = {
			name: id,
			description: commandOptions.description ?? commandOptions.title ?? "",
			argName: commandOptions.argName,
			execute: async (commandCtx) => {
				await handler(createCommandContext(commandCtx.args));
			},
		};
		return track(ctx.commands.register(command));
	};

	const registerTool: PluginAPI["registerTool"] = (tool) =>
		track(ctx.runtime.addTool(toAgentTool(tool)));

	return {
		logger,
		ui: ctx.ui,
		session,
		settings,
		model,
		system,
		on,
		registerCommand,
		registerTool,
		addSystemPrompt: (text) => track(ctx.runtime.addSystemPromptAddition(text)),
		addDebugSection: (key, lines) =>
			track(ctx.runtime.setDebugSection(key, lines)),
	};
}
