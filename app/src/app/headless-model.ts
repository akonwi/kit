import type { Api, Model } from "../runtime/agent";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { selectModelBySelector } from "../runtime/provider-selection";

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

export function resolveStartupModelSelector(
	explicitSelector: string | undefined,
	configuredDefault: string | undefined,
	isNewSession: boolean,
): string | undefined {
	return explicitSelector ?? (isNewSession ? configuredDefault : undefined);
}

export function selectStartupModel(
	models: Array<Model<Api>>,
	selector: string,
): Model<Api> {
	return selectModelBySelector(models, selector);
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
