import { createPluginAPI } from "./api";
import type { PluginContext, PluginDefinition, PluginDispose } from "./types";

type ManagedPlugin = {
	name: string;
	dispose: () => void;
};

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
		initializer: PluginDefinition,
	): void {
		const pluginName = requirePluginName(name);
		const disposers = new Set<PluginDispose>();
		let returnedDispose: PluginDispose | undefined;
		const managed: ManagedPlugin = {
			name: pluginName,
			dispose: () => {
				const remainingDisposers = [...disposers].reverse();
				disposers.clear();
				for (const disposer of remainingDisposers) {
					disposer();
				}
				returnedDispose?.();
				returnedDispose = undefined;
			},
		};

		this.plugins.push(managed);
		const api = createPluginAPI(this.ctx, {
			name: pluginName,
			addDisposer: (disposer) => {
				disposers.add(disposer);
				return () => {
					disposers.delete(disposer);
				};
			},
		});
		returnedDispose = initializer(api) ?? undefined;
	}
}
