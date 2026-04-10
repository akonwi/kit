import type { Plugin } from "./Plugin";
import type { PluginContext } from "./types";

export type PluginClass = new (ctx: PluginContext) => Plugin;

export class PluginManager {
	private readonly plugins: Plugin[] = [];

	constructor(
		private readonly pluginClasses: PluginClass[],
		private readonly ctx: PluginContext,
	) {}

	initialize(): void {
		for (const PluginClass of this.pluginClasses) {
			const plugin = new PluginClass(this.ctx);
			this.plugins.push(plugin);
			plugin.initialize();
		}
	}

	dispose(): void {
		for (const plugin of [...this.plugins].reverse()) {
			plugin.dispose();
		}
		this.plugins.length = 0;
	}
}
