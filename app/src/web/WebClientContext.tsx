/** @jsxImportSource solid-js */
import {
	type Accessor,
	createContext,
	createMemo,
	createSignal,
	type JSX,
	onCleanup,
	onMount,
	useContext,
} from "solid-js";
import type { TranscriptItem } from "../shell/transcript/turns";
import { WebClientController, type WebClientSnapshot } from "./controller";
import { protocolMessagesToTranscriptItems } from "./transcript-model";

type WebClientContextValue = {
	snapshot: Accessor<WebClientSnapshot>;
	transcriptItems: Accessor<TranscriptItem[]>;
	controller: WebClientController;
	focusComposer(): boolean;
	registerComposerFocus(handler: () => boolean): () => void;
};

const WebClientContext = createContext<WebClientContextValue>();

export function WebClientProvider(props: {
	children: JSX.Element;
}): JSX.Element {
	const controller = new WebClientController();
	const [snapshot, setSnapshot] = createSignal(controller.snapshot());
	const transcriptItems = createMemo(() =>
		protocolMessagesToTranscriptItems(snapshot().protocol.messages),
	);
	let unsubscribe: (() => void) | undefined;
	let composerFocus: (() => boolean) | null = null;
	const focusComposer = () => composerFocus?.() ?? false;
	const registerComposerFocus = (handler: () => boolean) => {
		composerFocus = handler;
		return () => {
			if (composerFocus === handler) composerFocus = null;
		};
	};

	onMount(() => {
		unsubscribe = controller.subscribe(setSnapshot);
		controller.start();
	});
	onCleanup(() => {
		unsubscribe?.();
		controller.dispose();
	});

	return (
		<WebClientContext.Provider
			value={{
				snapshot,
				transcriptItems,
				controller,
				focusComposer,
				registerComposerFocus,
			}}
		>
			{props.children}
		</WebClientContext.Provider>
	);
}

export function useWebClient(): WebClientContextValue {
	const value = useContext(WebClientContext);
	if (!value) throw new Error("WebClientProvider is missing");
	return value;
}
