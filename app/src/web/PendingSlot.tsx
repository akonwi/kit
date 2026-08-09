/** @jsxImportSource solid-js */
import { createMemo, type JSX, Show } from "solid-js";
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

	return (
		<div class="pending-slot">
			<span data-visually-hidden role="status" aria-live="polite">
				{announcement()}
			</span>
			<Show when={content()}>
				<div class="pending-display" aria-hidden="true">
					<span class="pending-spinner" />
					<span class="pending-content">{content()}</span>
				</div>
			</Show>
		</div>
	);
}
