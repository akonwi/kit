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
import type { WebToastSink } from "./web-toasts";

type WebClientContextValue = {
	snapshot: Accessor<WebClientSnapshot>;
	transcriptItems: Accessor<TranscriptItem[]>;
	controller: WebClientController;
	focusComposer(): boolean;
	restoreComposerMessages(messages: string[], operationId: string): boolean;
	registerComposerFocus(handler: () => boolean): () => void;
	registerComposerRestore(
		handler: (messages: string[], operationId: string) => boolean,
	): () => void;
};

const WebClientContext = createContext<WebClientContextValue>();

export function WebClientProvider(props: {
	children: JSX.Element;
	showToast?: WebToastSink;
}): JSX.Element {
	const controller = new WebClientController({ showToast: props.showToast });
	const [snapshot, setSnapshot] = createSignal(controller.snapshot());
	const transcriptItems = createMemo(() =>
		protocolMessagesToTranscriptItems(snapshot().protocol.messages),
	);
	let unsubscribe: (() => void) | undefined;
	let composerFocus: (() => boolean) | null = null;
	let composerRestore:
		| ((messages: string[], operationId: string) => boolean)
		| null = null;
	const focusComposer = () => composerFocus?.() ?? false;
	const restoreComposerMessages = (messages: string[], operationId: string) =>
		composerRestore?.(messages, operationId) ?? false;
	const registerComposerFocus = (handler: () => boolean) => {
		composerFocus = handler;
		return () => {
			if (composerFocus === handler) composerFocus = null;
		};
	};
	const registerComposerRestore = (
		handler: (messages: string[], operationId: string) => boolean,
	) => {
		composerRestore = handler;
		controller.resumeQueuedFollowUpRestore(handler);
		return () => {
			if (composerRestore === handler) composerRestore = null;
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
				restoreComposerMessages,
				registerComposerFocus,
				registerComposerRestore,
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
