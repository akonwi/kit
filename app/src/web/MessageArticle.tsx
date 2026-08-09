/** @jsxImportSource solid-js */
import { createMemo, Index, type JSX, Match, Switch } from "solid-js";
import { isRecord } from "./client-state";
import { displayValue, messageParts, messageRole } from "./presentation";

function MessagePart(props: {
	part: () => Record<string, unknown>;
}): JSX.Element {
	const imageUrl = createMemo(() => {
		const part = props.part();
		return part.type === "image" && typeof part.attachmentId === "string"
			? `/api/attachments/${encodeURIComponent(part.attachmentId)}`
			: null;
	});
	const text = createMemo(() => {
		const part = props.part();
		return typeof part.text === "string"
			? part.text
			: typeof part.thinking === "string"
				? part.thinking
				: displayValue(part);
	});

	return (
		<Switch>
			<Match when={imageUrl()}>
				{(url) => (
					<img
						src={url()}
						alt={
							typeof props.part().filename === "string"
								? (props.part().filename as string)
								: "Attached image"
						}
						loading="lazy"
					/>
				)}
			</Match>
			<Match when>
				<div
					class="message-content"
					classList={{ "message-thinking": props.part().type === "thinking" }}
				>
					{text()}
				</div>
			</Match>
		</Switch>
	);
}

export function MessageArticle(props: { message: () => unknown }): JSX.Element {
	const role = createMemo(() => messageRole(props.message()));
	const parts = createMemo(() => messageParts(props.message()));
	const emptyContent = createMemo(() => {
		const message = props.message();
		return isRecord(message) && message.type === "message_reference"
			? "Loading message…"
			: isRecord(message) && message.type === "message_unavailable"
				? "This message is too large to display."
				: displayValue(message);
	});

	return (
		<article class="message" data-role={role()}>
			<p class="message-label">
				{role() === "assistant" ? "Kit" : role() === "user" ? "You" : "Message"}
			</p>
			<Switch>
				<Match when={parts().length === 0}>
					<div class="message-content">{emptyContent()}</div>
				</Match>
				<Match when>
					<Index each={parts()}>{(part) => <MessagePart part={part} />}</Index>
				</Match>
			</Switch>
		</article>
	);
}
