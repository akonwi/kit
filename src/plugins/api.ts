import type { AgentTool } from "@mariozechner/pi-agent-core";
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
} from "./types";

export function createPluginAPI(
	ctx: PluginContext,
	options: {
		name: string;
		addDisposer: (disposer: PluginDispose) => void;
	},
): PluginAPI {
	function track(disposer: PluginDispose): PluginSubscription {
		let active = true;
		const wrapped = () => {
			if (!active) return;
			active = false;
			disposer();
		};
		options.addDisposer(wrapped);
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
		track(ctx.runtime.addTool(tool as unknown as AgentTool));

	return {
		logger,
		ui: ctx.ui,
		session,
		settings,
		system,
		on,
		registerCommand,
		registerTool,
		addSystemPrompt: (text) => track(ctx.runtime.addSystemPromptAddition(text)),
		setDebugSection: (key, lines) =>
			track(ctx.runtime.setDebugSection(key, lines)),
	};
}
