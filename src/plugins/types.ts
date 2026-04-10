import type { JSX } from "solid-js";
import type { CommandRegistry } from "../features/commands";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { LoadedSettings } from "../settings";

export type PluginUI = {
	notify: (message: string, type?: "info" | "warning" | "error") => void;
	custom: <T>(
		component: (props: { done: (result: T) => void }) => JSX.Element,
	) => Promise<T>;
};

export type PluginContext = {
	runtime: AgentRuntime;
	commands: CommandRegistry;
	settings: LoadedSettings;
	ui: PluginUI;
};
