import type { InternalPluginUI } from "../plugins/types";
import { getCurrentThemeConfig } from "../shell/theme";

async function noSelection(): Promise<undefined> {
	return undefined;
}

async function noCustomOverlay<T>(): Promise<T> {
	return undefined as T;
}

export function createHeadlessPluginUI(): InternalPluginUI {
	return {
		text: (text, style) => ({ __kitText: true, text, style }),
		theme: getCurrentThemeConfig,
		toast: () => {},
		select: noSelection,
		input: noSelection,
		confirm: async () => {
			throw new Error("interactivity is unavailable");
		},
		custom: noCustomOverlay,
		getTranscriptViewport: () => null,
	};
}
