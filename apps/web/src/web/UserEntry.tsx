/** @jsxImportSource solid-js */
import { For, type JSX, Show } from "solid-js";
import type {
	CodeReviewMessagePart,
	ImageMessagePart,
} from "../messages/parts";
import type { TranscriptItem } from "../shell/transcript/turns";
import {
	extractPromptCommandSynthetic,
	extractUserCustomParts,
	extractUserText,
} from "../shell/transcript/turns";
import { SafeMarkdown } from "./SafeMarkdown";

function imageSource(part: ImageMessagePart): string | null {
	if (part.attachmentId) {
		return `/api/attachments/${encodeURIComponent(part.attachmentId)}`;
	}
	if (part.data && /^image\/(png|jpeg|gif|webp)$/i.test(part.mimeType)) {
		return `data:${part.mimeType};base64,${part.data}`;
	}
	return null;
}

function CodeReviewPart(props: { part: CodeReviewMessagePart }): JSX.Element {
	const fileCount = props.part.review.files.length;
	const commentCount = props.part.review.files.reduce(
		(sum, file) =>
			sum + (file.fileComment.trim().length > 0 ? 1 : 0) + file.ranges.length,
		0,
	);
	return (
		<div class="user-attachment-summary">
			Code review · {commentCount} comment{commentCount === 1 ? "" : "s"} ·{" "}
			{fileCount} file{fileCount === 1 ? "" : "s"}
		</div>
	);
}

function ImagePart(props: { part: ImageMessagePart }): JSX.Element {
	const source = () => imageSource(props.part);
	const label = () => props.part.filename ?? "Image attachment";
	return (
		<Show
			when={source()}
			fallback={<div class="user-attachment-summary">{label()}</div>}
		>
			{(url) => (
				<a
					class="user-image-link"
					href={url()}
					target="_blank"
					rel="noopener noreferrer"
				>
					<img src={url()} alt={label()} loading="lazy" />
				</a>
			)}
		</Show>
	);
}

export function UserEntry(props: {
	item: Extract<TranscriptItem, { kind: "user" }>;
}): JSX.Element {
	const promptCommand = () => extractPromptCommandSynthetic(props.item.message);
	const text = () => extractUserText(props.item.message);
	const customParts = () => extractUserCustomParts(props.item.message);

	return (
		<article
			class="transcript-entry user-entry"
			classList={{ "is-aborted": props.item.aborted }}
		>
			<span data-visually-hidden>
				You: {props.item.aborted ? "aborted. " : ""}
			</span>
			<Show
				when={promptCommand()}
				fallback={
					<Show when={text().trim()}>
						<SafeMarkdown content={text()} />
					</Show>
				}
			>
				{(prompt) => (
					<div class="prompt-command">
						/{prompt().command}
						{prompt().args?.trim() ? ` ${prompt().args?.trim()}` : ""}
					</div>
				)}
			</Show>
			<For each={customParts()}>
				{(part) => {
					switch (part.type) {
						case "code-review":
							return <CodeReviewPart part={part} />;
						case "image":
							return <ImagePart part={part} />;
						default:
							return null;
					}
				}}
			</For>
		</article>
	);
}
