import { createPluginAPI } from "./api";
import type {
	Disposer,
	InternalPluginDefinition,
	Plugin,
	PluginContext,
} from "./types";

export type PluginErrorHandler = (input: {
	name: string;
	error: unknown;
}) => void;

type BasePluginRegistration = {
	name: string;
	continueOnError?: boolean;
	onError?: PluginErrorHandler;
	checkContributionConflicts?: boolean;
};

export type ExternalPluginRegistration = BasePluginRegistration & {
	initialize: Plugin;
	internalUi?: false;
};

export type InternalPluginRegistration = BasePluginRegistration & {
	initialize: InternalPluginDefinition;
	internalUi: true;
};

export type PluginRegistration =
	| ExternalPluginRegistration
	| InternalPluginRegistration;

export type PluginManagerInput = Plugin | PluginRegistration;

type ManagedPlugin = {
	name: string;
	takeDisposers: () => Disposer[];
};

function startDisposer(
	pluginName: string,
	disposer: Disposer,
): Promise<void> | null {
	try {
		const result = (disposer as () => unknown)();
		if (!(result instanceof Promise)) return null;
		return result.catch((error) => {
			console.error(
				`[plugin:${pluginName}] cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
			);
		});
	} catch (error) {
		console.error(
			`[plugin:${pluginName}] cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
		);
		return null;
	}
}

async function runDisposer(
	pluginName: string,
	disposer: Disposer,
): Promise<void> {
	try {
		await (disposer as () => unknown)();
	} catch (error) {
		console.error(
			`[plugin:${pluginName}] cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
		);
	}
}

function requirePluginName(name: string | undefined): string {
	const trimmed = name?.trim();
	if (!trimmed) {
		throw new Error("Plugin name is required.");
	}
	return trimmed;
}

function normalizePluginInput(input: PluginManagerInput): PluginRegistration {
	if (typeof input === "function") {
		return {
			name: requirePluginName(input.name),
			initialize: input,
		};
	}
	return {
		...input,
		name: requirePluginName(input.name),
	};
}

export class PluginManager {
	private readonly plugins: ManagedPlugin[] = [];

	constructor(
		private readonly pluginDefinitions: PluginManagerInput[],
		private readonly ctx: PluginContext,
	) {}

	initialize(): void {
		for (const definition of this.pluginDefinitions) {
			this.initializePlugin(normalizePluginInput(definition));
		}
	}

	dispose(): void {
		for (const plugin of this.takePluginsForDisposal()) {
			for (const disposer of plugin.takeDisposers()) {
				const pending = startDisposer(plugin.name, disposer);
				if (pending) void pending;
			}
		}
	}

	async disposeAsync(): Promise<void> {
		for (const plugin of this.takePluginsForDisposal()) {
			for (const disposer of plugin.takeDisposers()) {
				await runDisposer(plugin.name, disposer);
			}
		}
	}

	private takePluginsForDisposal(): ManagedPlugin[] {
		const plugins = [...this.plugins].reverse();
		this.plugins.length = 0;
		return plugins;
	}

	private initializePlugin(registration: PluginRegistration): void {
		const pluginName = registration.name;
		const disposers = new Set<Disposer>();
		let returnedDispose: Disposer | undefined;
		const managed: ManagedPlugin = {
			name: pluginName,
			takeDisposers: () => {
				const remainingDisposers = [...disposers].reverse();
				disposers.clear();
				if (returnedDispose) remainingDisposers.push(returnedDispose);
				returnedDispose = undefined;
				return remainingDisposers;
			},
		};

		this.plugins.push(managed);
		const commonOptions = {
			name: pluginName,
			checkContributionConflicts: registration.checkContributionConflicts,
			addDisposer: (disposer: Disposer) => {
				disposers.add(disposer);
				return () => {
					disposers.delete(disposer);
				};
			},
		};
		try {
			if (registration.internalUi) {
				const api = createPluginAPI(this.ctx, {
					...commonOptions,
					exposeInternalUi: true,
				});
				returnedDispose = registration.initialize(api) ?? undefined;
			} else {
				const api = createPluginAPI(this.ctx, {
					...commonOptions,
					exposeInternalUi: false,
				});
				returnedDispose = registration.initialize(api) ?? undefined;
			}
		} catch (error) {
			for (const disposer of managed.takeDisposers()) {
				const pending = startDisposer(pluginName, disposer);
				if (pending) void pending;
			}
			const index = this.plugins.indexOf(managed);
			if (index >= 0) this.plugins.splice(index, 1);
			if (!registration.continueOnError) throw error;
			registration.onError?.({ name: pluginName, error });
		}
	}
}
