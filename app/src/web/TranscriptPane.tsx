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
import {
	type DisplayItem,
	displayItemKey,
	groupItemsForDisplay,
} from "../shell/transcript/turns";
import { TurnEntry } from "./TurnEntry";
import { liveToolsForTranscriptItems } from "./transcript-model";
import { useWebClient } from "./WebClientContext";

type ScrollAnchor = {
	height: number;
	top: number;
};

export function TranscriptPane(): JSX.Element {
	const { snapshot, transcriptItems, controller } = useWebClient();
	let transcript: HTMLElement | undefined;
	let stickToBottom = true;
	let scrollAnchor: ScrollAnchor | null = null;
	let scrollFrame: number | null = null;
	const protocol = createMemo(() => snapshot().protocol);
	const messages = createMemo(() => protocol().messages);
	const messageOffset = createMemo(() => protocol().messageOffset);
	const lastMessage = createMemo(() => messages().at(-1));
	const lastTool = createMemo(() => protocol().tools.at(-1));
	const displayItems = createMemo<DisplayItem[]>((previous) =>
		groupItemsForDisplay(transcriptItems(), protocol().activeTurnId, previous),
	);
	const displayItemKeys = createMemo(() => displayItems().map(displayItemKey));
	const displayItemsByKey = createMemo(
		() => new Map(displayItems().map((item) => [displayItemKey(item), item])),
	);
	const liveToolsForDisplayItem = (displayItem: DisplayItem | undefined) => {
		if (!displayItem) return [];
		const selectedItems =
			displayItem.kind === "turn-work" ? displayItem.items : [displayItem.item];
		return liveToolsForTranscriptItems(
			selectedItems,
			transcriptItems(),
			protocol().tools,
			protocol().activeTurnId,
		);
	};

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
			class="transcript-pane"
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
			<Show when={protocol().messageKeys.length === 0}>
				<div class="empty-state">
					<strong>k&nbsp;&nbsp;&nbsp;&nbsp;i&nbsp;&nbsp;&nbsp;&nbsp;t</strong>
					<span>Ask a question or give a task.</span>
				</div>
			</Show>
			<div class="transcript-list">
				<For each={displayItemKeys()}>
					{(key) => {
						const displayItem = createMemo(() => displayItemsByKey().get(key));
						return (
							<Show when={displayItem()}>
								<TurnEntry
									displayItem={displayItem}
									liveTools={liveToolsForDisplayItem(displayItem())}
								/>
							</Show>
						);
					}}
				</For>
			</div>
		</main>
	);
}
