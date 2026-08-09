/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	For,
	type JSX,
	on,
	onCleanup,
	onMount,
	Show,
} from "solid-js";
import { ActivitySection } from "./ActivitySection";
import { MessageArticle } from "./MessageArticle";
import { useWebClient } from "./WebClientContext";

type ScrollAnchor = {
	height: number;
	top: number;
};

export function Transcript(): JSX.Element {
	const { snapshot, controller } = useWebClient();
	let transcript: HTMLElement | undefined;
	let stickToBottom = true;
	let scrollAnchor: ScrollAnchor | null = null;
	let scrollFrame: number | null = null;
	const protocol = createMemo(() => snapshot().protocol);
	const messages = createMemo(() => protocol().messages);
	const messageKeys = createMemo(() => protocol().messageKeys);
	const messageOffset = createMemo(() => protocol().messageOffset);
	const lastMessage = createMemo(() => messages().at(-1));
	const lastTool = createMemo(() => protocol().tools.at(-1));
	const messagesByKey = createMemo(() => {
		const byKey = new Map<string, unknown>();
		for (const [index, key] of messageKeys().entries()) {
			byKey.set(key, messages()[index]);
		}
		return byKey;
	});

	const scheduleScroll = () => {
		if (!transcript || scrollFrame !== null) return;
		scrollFrame = requestAnimationFrame(() => {
			scrollFrame = null;
			if (!transcript) return;
			if (scrollAnchor) {
				transcript.scrollTop =
					scrollAnchor.top + (transcript.scrollHeight - scrollAnchor.height);
				scrollAnchor = null;
				return;
			}
			if (stickToBottom) transcript.scrollTop = transcript.scrollHeight;
		});
	};

	createEffect(on([lastMessage, messageOffset, lastTool], scheduleScroll));

	let resizeObserver: ResizeObserver | null = null;
	onMount(() => {
		if (transcript) {
			resizeObserver = new ResizeObserver(scheduleScroll);
			resizeObserver.observe(transcript);
		}
		window.visualViewport?.addEventListener("resize", scheduleScroll);
	});
	onCleanup(() => {
		if (scrollFrame !== null) cancelAnimationFrame(scrollFrame);
		resizeObserver?.disconnect();
		window.visualViewport?.removeEventListener("resize", scheduleScroll);
	});

	const loadEarlier = () => {
		void controller.loadEarlier(() => {
			if (!transcript) return;
			scrollAnchor = {
				height: transcript.scrollHeight,
				top: transcript.scrollTop,
			};
		});
	};

	return (
		<main
			ref={transcript}
			id="transcript"
			class="transcript"
			tabIndex={0}
			aria-label="Conversation transcript"
			onScroll={() => {
				if (!transcript) return;
				stickToBottom =
					transcript.scrollHeight -
						transcript.scrollTop -
						transcript.clientHeight <
					96;
			}}
		>
			<Show when={protocol().messageOffset > 0}>
				<button
					class="load-earlier"
					data-variant="ghost"
					data-size="small"
					type="button"
					disabled={snapshot().loadingEarlier || protocol().phase !== "live"}
					onClick={loadEarlier}
				>
					{snapshot().loadingEarlier ? "Loading…" : "Load earlier messages"}
				</button>
			</Show>
			<Show when={messageKeys().length === 0}>
				<div class="empty-state">
					<strong>k&nbsp;&nbsp;&nbsp;&nbsp;i&nbsp;&nbsp;&nbsp;&nbsp;t</strong>
					<span>Start a conversation with your coding agent.</span>
				</div>
			</Show>
			<div class="message-list">
				<For each={messageKeys()}>
					{(key) => {
						const message = createMemo(() => messagesByKey().get(key));
						return <MessageArticle message={message} />;
					}}
				</For>
			</div>
			<ActivitySection />
		</main>
	);
}
