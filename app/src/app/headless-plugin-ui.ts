import type { InternalPluginUI } from "../plugins/types";
import { getCurrentThemeConfig } from "../shell/theme";
import type { RemoteInteractionBroker } from "./remote-interaction-broker";

async function noSelection(): Promise<undefined> {
	return undefined;
}

async function noCustomOverlay<T>(): Promise<T> {
	return undefined as T;
}

export function createHeadlessPluginUI(
	interactions?: RemoteInteractionBroker,
): InternalPluginUI {
	return {
		text: (text, style) => ({ __kitText: true, text, style }),
		theme: getCurrentThemeConfig,
		toast: interactions ? interactions.toast.bind(interactions) : () => {},
		select: interactions ? interactions.select.bind(interactions) : noSelection,
		input: interactions ? interactions.input.bind(interactions) : noSelection,
		confirm: interactions
			? interactions.confirm.bind(interactions)
			: async () => {
					throw new Error("interactivity is unavailable");
				},
		custom: noCustomOverlay,
		interaction: noCustomOverlay,
		getTranscriptViewport: () => null,
	};
}
