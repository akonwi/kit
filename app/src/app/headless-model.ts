import type { Api, Model } from "../runtime/agent";
import type { AgentRuntime } from "../runtime/agent-runtime";

export class StartupModelAuthenticationRequiredError extends Error {
	constructor(message: string) {
		super(message);
		this.name = "StartupModelAuthenticationRequiredError";
	}
}

export function isValidModelSelector(selector: string): boolean {
	return (
		selector.includes("/") &&
		!selector.startsWith("/") &&
		!selector.endsWith("/")
	);
}

export function selectStartupModel(
	models: Array<Model<Api>>,
	selector: string,
): Model<Api> {
	const separator = selector.indexOf("/");
	const provider = selector.slice(0, separator);
	const modelId = selector.slice(separator + 1);
	const model = models.find(
		(candidate) => candidate.provider === provider && candidate.id === modelId,
	);
	if (model) return model;

	const providerModels = models.filter(
		(candidate) => candidate.provider === provider,
	);
	if (providerModels.length === 0) {
		const providers = [
			...new Set(models.map((candidate) => candidate.provider)),
		];
		throw new Error(
			`Provider not available: ${provider}. Available authenticated providers: ${providers.join(", ") || "none"}`,
		);
	}
	throw new Error(
		`Model not found: ${selector}. Available ${provider} models: ${providerModels.map((candidate) => candidate.id).join(", ")}`,
	);
}

export async function applyStartupModel(
	runtime: Pick<
		AgentRuntime,
		"getAvailableModels" | "setModel" | "waitForModelAdaptation"
	>,
	selector: string | undefined,
	providerAuth?: {
		isKnown(provider: string): boolean;
		isAuthenticated(provider: string): boolean;
	},
): Promise<void> {
	if (!selector) return;
	const provider = selector.slice(0, selector.indexOf("/"));
	if (
		providerAuth?.isKnown(provider) &&
		!providerAuth.isAuthenticated(provider)
	) {
		throw new StartupModelAuthenticationRequiredError(
			`Authenticate with ${provider} to use ${selector}.`,
		);
	}
	const model = selectStartupModel(runtime.getAvailableModels(), selector);
	runtime.setModel(model);
	await runtime.waitForModelAdaptation();
}
