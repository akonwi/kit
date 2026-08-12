/** @jsxImportSource solid-js */
import { createMemo, For, type JSX, Show } from "solid-js";
import { isRecord } from "./client-state";
import { useWebClient } from "./WebClientContext";

export function PendingSlot(): JSX.Element {
	const { snapshot } = useWebClient();
	const protocol = createMemo(() => snapshot().protocol);
	const streaming = createMemo(
		() => protocol().serverState.isStreaming === true,
	);
	const content = createMemo(() => {
		if (!streaming()) return "";
		const pendingStatus = protocol().pendingStatus;
		if (pendingStatus?.startsWith("Retrying")) return pendingStatus;
		const activeIndex = protocol().activeMessageIndex;
		const activeMessage =
			typeof activeIndex === "number" ? protocol().messages[activeIndex] : null;
		if (!isRecord(activeMessage) || !Array.isArray(activeMessage.content)) {
			return pendingStatus ?? "Working…";
		}
		for (let index = activeMessage.content.length - 1; index >= 0; index -= 1) {
			const part = activeMessage.content[index];
			if (!isRecord(part)) continue;
			if (part.type === "text" && typeof part.text === "string" && part.text) {
				return "Working…";
			}
			if (
				part.type === "thinking" &&
				typeof part.thinking === "string" &&
				part.thinking.trim()
			) {
				return part.thinking;
			}
		}
		return pendingStatus ?? "Working…";
	});

	const announcement = createMemo(() =>
		content().startsWith("Retrying")
			? content()
			: streaming()
				? "Kit is working"
				: "",
	);
	const hiddenQueueCount = createMemo(() =>
		Math.max(
			0,
			protocol().queuedMessageCount - protocol().queuedMessagePreviews.length,
		),
	);
	const empty = createMemo(
		() =>
			!content() &&
			protocol().queuedMessagePreviews.length === 0 &&
			hiddenQueueCount() === 0,
	);

	return (
		<div class="pending-slot" classList={{ "is-empty": empty() }}>
			<span data-visually-hidden role="status" aria-live="polite">
				{announcement()}
			</span>
			<Show when={content()}>
				<div class="pending-display" aria-hidden="true">
					<span class="pending-spinner" />
					<span class="pending-content">{content()}</span>
				</div>
			</Show>
			<Show when={protocol().queuedMessagePreviews.length > 0}>
				<ol
					class="pending-followups"
					role="list"
					aria-label="Queued follow-up messages"
				>
					<For each={protocol().queuedMessagePreviews}>
						{(preview, index) => (
							<li class="pending-followup">
								<span class="pending-followup-label">
									Follow-up {index() + 1}:
								</span>
								<span class="pending-followup-preview">{preview}</span>
							</li>
						)}
					</For>
				</ol>
			</Show>
			<Show when={hiddenQueueCount() > 0}>
				<div class="pending-followup-more">
					+{hiddenQueueCount()} more queued
				</div>
			</Show>
		</div>
	);
}
