import { createPluginAPI } from "./api";
import { Plugin } from "./Plugin";
import type { PluginContext, PluginDispose, PluginInitializer } from "./types";

export type PluginClass = new (ctx: PluginContext) => Plugin;
export type PluginDefinition = PluginClass | PluginInitializer;

type ManagedPlugin = {
	name: string;
	dispose: () => void;
};

function isPluginClass(
	definition: PluginDefinition,
): definition is PluginClass {
	return (
		typeof definition === "function" && definition.prototype instanceof Plugin
	);
}

function requirePluginName(name: string | undefined): string {
	const trimmed = name?.trim();
	if (!trimmed) {
		throw new Error("Plugin name is required.");
	}
	return trimmed;
}

export class PluginManager {
	private readonly plugins: ManagedPlugin[] = [];

	constructor(
		private readonly pluginDefinitions: PluginDefinition[],
		private readonly ctx: PluginContext,
	) {}

	initialize(): void {
		for (const definition of this.pluginDefinitions) {
			if (isPluginClass(definition)) {
				const name = requirePluginName(definition.name);
				const plugin = new definition(this.ctx);
				this.plugins.push({ name, dispose: () => plugin.dispose() });
				plugin.initialize();
				continue;
			}

			this.initializeFunctionPlugin(definition.name, definition);
		}
	}

	dispose(): void {
		for (const plugin of [...this.plugins].reverse()) {
			plugin.dispose();
		}
		this.plugins.length = 0;
	}

	private initializeFunctionPlugin(
		name: string | undefined,
		initializer: PluginInitializer,
	): void {
		const pluginName = requirePluginName(name);
		const disposers: PluginDispose[] = [];
		let returnedDispose: PluginDispose | undefined;
		const managed: ManagedPlugin = {
			name: pluginName,
			dispose: () => {
				for (const disposer of disposers.splice(0).reverse()) {
					disposer();
				}
				returnedDispose?.();
				returnedDispose = undefined;
			},
		};

		this.plugins.push(managed);
		const api = createPluginAPI(this.ctx, {
			name: pluginName,
			addDisposer: (disposer) => disposers.push(disposer),
		});
		returnedDispose = initializer(api) ?? undefined;
	}
}
