import type { StreamFn } from "@earendil-works/pi-agent-core";
import type { Api, Model } from "@earendil-works/pi-ai";
import { registerBunOAuthFlows } from "@earendil-works/pi-ai/bun-oauth";
import { builtinModels } from "@earendil-works/pi-ai/providers/all";
import { kitCredentialStore } from "../auth";

// Dynamic imports cannot discover OAuth adapters from a compiled Bun binary.
// Register the statically bundled flows before constructing providers.
registerBunOAuthFlows();

export const kitModels = builtinModels({ credentials: kitCredentialStore });

let availableModelKeys: Set<string> | null = null;
let availableProviderIds: Set<string> | null = null;

function modelKey(model: Model<Api>): string {
	return `${model.provider}\u0000${model.id}`;
}

export async function refreshModelAvailability(): Promise<void> {
	const available = await kitModels.getAvailable();
	availableModelKeys = new Set(available.map(modelKey));
	availableProviderIds = new Set(available.map((model) => model.provider));
}

export function hasCachedProviderAuth(providerId: string): boolean {
	return availableProviderIds?.has(providerId) ?? false;
}

export function isModelAvailable(model: Model<Api>): boolean {
	return availableModelKeys?.has(modelKey(model)) ?? true;
}

export const kitStreamFn: StreamFn = (model, context, options) =>
	kitModels.streamSimple(model, context, options);

export async function hasModelAuth(model: Model<Api>): Promise<boolean> {
	return (await kitModels.getAuth(model)) !== undefined;
}
