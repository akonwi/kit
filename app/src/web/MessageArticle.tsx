/** @jsxImportSource solid-js */
import { createMemo, Index, type JSX, Match, Show, Switch } from "solid-js";
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
				<div class="message-content">{text()}</div>
			</Match>
		</Switch>
	);
}

function ToolResultEntry(props: { message: () => unknown }): JSX.Element {
	const record = createMemo<Record<string, unknown> | null>(() => {
		const message = props.message();
		return isRecord(message) ? message : null;
	});
	const name = createMemo(() => {
		const value = record()?.toolName;
		return typeof value === "string" && value.trim() ? value : "Tool result";
	});
	const parts = createMemo(() => messageParts(props.message()));

	return (
		<details
			class="standalone-tool-result"
			data-error={String(record()?.isError === true)}
		>
			<summary>{name()}</summary>
			<div class="standalone-tool-result-body">
				<Show
					when={parts().length > 0}
					fallback={
						<div class="tool-result">{displayValue(props.message())}</div>
					}
				>
					<Index each={parts()}>{(part) => <MessagePart part={part} />}</Index>
				</Show>
			</div>
		</details>
	);
}

export function MessageArticle(props: { message: () => unknown }): JSX.Element {
	const role = createMemo(() => messageRole(props.message()));
	const parts = createMemo(() =>
		messageParts(props.message()).filter((part) => part.type !== "thinking"),
	);
	const emptyContent = createMemo(() => {
		const message = props.message();
		if (isRecord(message) && message.type === "message_reference") {
			return "Loading message…";
		}
		if (isRecord(message) && message.type === "message_unavailable") {
			return "This message is too large to display.";
		}
		if (isRecord(message) && Array.isArray(message.content)) return "";
		return displayValue(message);
	});
	const visible = createMemo(
		() => parts().length > 0 || emptyContent().length > 0,
	);

	return (
		<Show
			when={role() !== "toolResult"}
			fallback={<ToolResultEntry message={props.message} />}
		>
			<Show when={visible()}>
				<article class="message" data-role={role()}>
					<Show when={role() === "assistant" || role() === "user"}>
						<span data-visually-hidden>
							{role() === "assistant" ? "Kit: " : "You: "}
						</span>
					</Show>
					<Show when={role() !== "assistant" && role() !== "user"}>
						<p class="message-label">Message</p>
					</Show>
					<Switch>
						<Match when={parts().length === 0}>
							<div class="message-content">{emptyContent()}</div>
						</Match>
						<Match when>
							<Index each={parts()}>
								{(part) => <MessagePart part={part} />}
							</Index>
						</Match>
					</Switch>
				</article>
			</Show>
		</Show>
	);
}
