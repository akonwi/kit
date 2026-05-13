import { createPluginAPI } from "./api";
import type {
	InternalPluginDefinition,
	PluginContext,
	PluginDefinition,
	PluginDispose,
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
	initialize: PluginDefinition;
	internalUi?: false;
};

export type InternalPluginRegistration = BasePluginRegistration & {
	initialize: InternalPluginDefinition;
	internalUi: true;
};

export type PluginRegistration =
	| ExternalPluginRegistration
	| InternalPluginRegistration;

export type PluginManagerInput = PluginDefinition | PluginRegistration;

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
		for (const plugin of [...this.plugins].reverse()) {
			plugin.dispose();
		}
		this.plugins.length = 0;
	}

	private initializePlugin(registration: PluginRegistration): void {
		const pluginName = registration.name;
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
		const commonOptions = {
			name: pluginName,
			checkContributionConflicts: registration.checkContributionConflicts,
			addDisposer: (disposer: PluginDispose) => {
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
			managed.dispose();
			const index = this.plugins.indexOf(managed);
			if (index >= 0) this.plugins.splice(index, 1);
			if (!registration.continueOnError) throw error;
			registration.onError?.({ name: pluginName, error });
		}
	}
}
