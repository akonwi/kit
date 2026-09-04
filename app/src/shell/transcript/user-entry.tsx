import { TextAttributes } from "@opentui/core";
import { For, Show } from "solid-js";
import type {
	CodeReviewMessagePart,
	ImageMessagePart,
	UserMultipartMessage,
} from "../../messages/parts";
import type { UserMessage } from "../../runtime/agent";
import { IMAGE, PENCIL } from "../glyphs";
import { KitMarkdown } from "../KitMarkdown";
import { theme } from "../theme";
import { createMessageContextMenuGesture } from "./message-context-menu";
import {
	extractPromptCommandSynthetic,
	extractUserCustomParts,
	extractUserMarkdownSource,
	extractUserText,
} from "./turns";
import type { OpenImage, OpenMessageContextMenu } from "./types";

const ABORTED_ATTRS = TextAttributes.DIM | TextAttributes.STRIKETHROUGH;

function CodeReviewPartEntry(props: {
	part: CodeReviewMessagePart;
	aborted?: boolean;
}) {
	const review = props.part.review;
	const fileCount = review.files.length;
	const commentCount = review.files.reduce(
		(sum, file) =>
			sum + (file.fileComment.trim().length > 0 ? 1 : 0) + file.ranges.length,
		0,
	);
	const summary = `Code review · ${commentCount} comment${commentCount === 1 ? "" : "s"} · ${fileCount} file${fileCount === 1 ? "" : "s"}`;

	return (
		<text
			fg={props.aborted ? theme.textMuted : theme.attachmentText}
			attributes={props.aborted ? ABORTED_ATTRS : undefined}
		>
			{PENCIL} {summary}
		</text>
	);
}

function ImagePartEntry(props: {
	id: string;
	part: ImageMessagePart;
	aborted?: boolean;
	onOpen?: OpenImage;
}) {
	const label = props.part.filename ?? "Image attachment";
	return (
		<box
			width="100%"
			onMouseUp={
				props.onOpen
					? (event) => {
							if (event.button !== 0 || props.aborted) return;
							event.stopPropagation();
							props.onOpen?.({
								id: props.id,
								image: {
									data: props.part.data,
									mimeType: props.part.mimeType,
									filename: label,
									sourcePath: props.part.sourcePath,
								},
							});
						}
					: undefined
			}
		>
			<text fg={props.aborted ? theme.textMuted : theme.attachmentText}>
				{IMAGE} {label}
			</text>
		</box>
	);
}

function UserTextEntry(props: { text: string; aborted?: boolean }) {
	return (
		<KitMarkdown
			content={props.text}
			fg={props.aborted ? theme.textMuted : theme.textPrimary}
		/>
	);
}

function PromptCommandEntry(props: {
	command: string;
	args?: string;
	aborted?: boolean;
}) {
	const suffix = props.args?.trim().length ? ` ${props.args?.trim()}` : "";
	return (
		<text fg={props.aborted ? theme.textMuted : theme.textPrimary}>
			{`/${props.command}${suffix}`}
		</text>
	);
}

export function UserEntry(props: {
	itemId: string;
	msg: UserMessage | UserMultipartMessage;
	aborted?: boolean;
	openImage?: OpenImage;
	openMessageContextMenu?: OpenMessageContextMenu;
}) {
	const promptCommand = extractPromptCommandSynthetic(props.msg);
	const text = extractUserText(props.msg);
	const markdown = extractUserMarkdownSource(props.msg);
	const messageContextMenuGesture = props.openMessageContextMenu
		? createMessageContextMenuGesture(
				() => markdown,
				props.openMessageContextMenu,
			)
		: {};
	const parts = extractUserCustomParts(props.msg);
	return (
		<box
			border={["left"]}
			borderColor={props.aborted ? theme.textMuted : theme.userBorder}
			paddingLeft={1}
			flexDirection="column"
			gap={1}
			width="100%"
			{...messageContextMenuGesture}
		>
			<Show
				when={promptCommand}
				fallback={
					<Show when={text.trim().length > 0}>
						<UserTextEntry text={text} aborted={props.aborted} />
					</Show>
				}
			>
				{(prompt) => (
					<PromptCommandEntry
						command={prompt().command}
						args={prompt().args}
						aborted={props.aborted}
					/>
				)}
			</Show>
			<For each={parts}>
				{(part, index) => {
					switch (part.type) {
						case "code-review":
							return (
								<CodeReviewPartEntry part={part} aborted={props.aborted} />
							);
						case "image":
							return (
								<ImagePartEntry
									id={`${props.itemId}:${index()}`}
									part={part}
									aborted={props.aborted}
									onOpen={props.openImage}
								/>
							);
						default:
							return null;
					}
				}}
			</For>
		</box>
	);
}
