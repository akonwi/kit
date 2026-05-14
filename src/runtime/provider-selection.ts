import {
	type Api,
	getEnvApiKey,
	getModels,
	getProviders,
	type KnownProvider,
	type Model,
} from "@earendil-works/pi-ai";

type ProviderSelectionOptions<
	TProvider extends string,
	TModel extends { id: string },
> = {
	providerIds: readonly TProvider[];
	hasEnvApiKey: (provider: TProvider) => string | undefined | null;
	getModelsForProvider: (provider: TProvider) => readonly TModel[];
};

export function listAuthenticatedProviders<
	TProvider extends string,
	TModel extends { id: string },
>(
	authenticatedProviderIds: string[],
	options: ProviderSelectionOptions<TProvider, TModel>,
): TProvider[] {
	const availableProviders = new Set(options.providerIds);
	const fromAuth = [...new Set(authenticatedProviderIds)].filter(
		(provider): provider is TProvider =>
			availableProviders.has(provider as TProvider),
	);
	const fromEnv = options.providerIds.filter(
		(provider) =>
			!fromAuth.includes(provider) && options.hasEnvApiKey(provider) != null,
	);
	return [...fromAuth, ...fromEnv].filter(
		(provider) => options.getModelsForProvider(provider).length > 0,
	);
}

export function selectDefaultModel<
	TProvider extends string,
	TModel extends { id: string },
>(
	authenticatedProviderIds: string[],
	preferredModelId: string | undefined,
	options: ProviderSelectionOptions<TProvider, TModel>,
): TModel | undefined {
	const providers = listAuthenticatedProviders(
		authenticatedProviderIds,
		options,
	);

	if (preferredModelId) {
		for (const provider of providers) {
			for (const model of options.getModelsForProvider(provider)) {
				if (model.id === preferredModelId) return model;
			}
		}
	}

	for (const provider of providers) {
		const model = options.getModelsForProvider(provider)[0];
		if (model) return model;
	}

	return undefined;
}

function getRuntimeProviderSelectionOptions() {
	return {
		providerIds: getProviders(),
		hasEnvApiKey: (provider: KnownProvider) => getEnvApiKey(provider),
		getModelsForProvider: (provider: KnownProvider) => getModels(provider),
	};
}

export function listRegisteredAuthenticatedProviders(
	authenticatedProviderIds: string[],
): KnownProvider[] {
	return listAuthenticatedProviders(
		authenticatedProviderIds,
		getRuntimeProviderSelectionOptions(),
	);
}

export function resolveDefaultAuthenticatedModel(
	authenticatedProviderIds: string[],
	preferredModelId?: string,
): Model<Api> | undefined {
	return selectDefaultModel(
		authenticatedProviderIds,
		preferredModelId,
		getRuntimeProviderSelectionOptions(),
	);
}
