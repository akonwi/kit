import type { Plugin } from "./Plugin";
import type { PluginContext } from "./types";

export type PluginClass = new (ctx: PluginContext) => Plugin;

export class PluginManager {
	private readonly plugins: Plugin[] = [];

	constructor(
		private readonly pluginClasses: PluginClass[],
		private readonly ctx: PluginContext,
	) {}

	async initialize(): Promise<void> {
		for (const PluginClass of this.pluginClasses) {
			const plugin = new PluginClass(this.ctx);
			this.plugins.push(plugin);
			await plugin.initialize();
		}
	}

	async dispose(): Promise<void> {
		for (const plugin of [...this.plugins].reverse()) {
			await plugin.dispose();
		}
		this.plugins.length = 0;
	}
}
