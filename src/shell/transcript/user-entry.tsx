import type { UserMessage } from "@mariozechner/pi-ai";
import type { BorderSides } from "@opentui/core";
import { TextAttributes } from "@opentui/core";
import { For, Show } from "solid-js";
import { openImagePart } from "../../features/images/open";
import type {
	CodeReviewMessagePart,
	ImageMessagePart,
	UserMultipartMessage,
} from "../../messages/parts";
import { CIRCLE_EMPTY } from "../glyphs";
import { syntaxStyle, theme } from "../theme";
import {
	extractPromptCommandSynthetic,
	extractUserCustomParts,
	extractUserText,
} from "./turns";
import type { TranscriptToast } from "./types";

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
		<box
			border={["left" as BorderSides]}
			borderColor={props.aborted ? theme.textMuted : theme.attachmentText}
			paddingLeft={1}
			flexDirection="column"
			gap={0}
			width="100%"
		>
			<text
				fg={props.aborted ? theme.textMuted : theme.attachmentText}
				attributes={props.aborted ? ABORTED_ATTRS : undefined}
			>
				{CIRCLE_EMPTY} {summary}
			</text>
		</box>
	);
}

function ImagePartEntry(props: {
	part: ImageMessagePart;
	aborted?: boolean;
	showToast: (toast: TranscriptToast) => void;
}) {
	const label = props.part.filename ?? "Image attachment";
	return (
		<box
			border={["left" as BorderSides]}
			borderColor={props.aborted ? theme.textMuted : theme.attachmentText}
			paddingLeft={1}
			flexDirection="column"
			gap={0}
			width="100%"
			onMouseUp={() => {
				if (props.aborted) return;
				void openImagePart(props.part).then((result) => {
					if (result.ok) return;
					props.showToast({
						title: "Could not open image",
						subtitle: result.message,
						variant: "error",
					});
				});
			}}
		>
			<text fg={props.aborted ? theme.textMuted : theme.attachmentText}>
				🖼️ {label}
			</text>
		</box>
	);
}

function UserTextEntry(props: { text: string; aborted?: boolean }) {
	return (
		<box
			border={["left" as BorderSides]}
			borderColor={props.aborted ? theme.textMuted : theme.userBorder}
			paddingLeft={1}
			flexDirection="column"
			gap={0}
			width="100%"
		>
			<markdown
				content={props.text}
				syntaxStyle={syntaxStyle()}
				conceal
				fg={props.aborted ? theme.textMuted : theme.textPrimary}
			/>
		</box>
	);
}

function PromptCommandEntry(props: {
	command: string;
	args?: string;
	aborted?: boolean;
}) {
	const suffix = props.args?.trim().length ? ` ${props.args?.trim()}` : "";
	return (
		<box
			border={["left" as BorderSides]}
			borderColor={props.aborted ? theme.textMuted : theme.userBorder}
			paddingLeft={1}
			flexDirection="column"
			gap={0}
			width="100%"
		>
			<text fg={props.aborted ? theme.textMuted : theme.textPrimary}>
				{`/${props.command}${suffix}`}
			</text>
		</box>
	);
}

export function UserEntry(props: {
	msg: UserMessage | UserMultipartMessage;
	aborted?: boolean;
	showToast: (toast: TranscriptToast) => void;
}) {
	const promptCommand = extractPromptCommandSynthetic(props.msg);
	const text = extractUserText(props.msg);
	const parts = extractUserCustomParts(props.msg);
	return (
		<box flexDirection="column" gap={1} width="100%">
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
				{(part) => {
					switch (part.type) {
						case "code-review":
							return (
								<CodeReviewPartEntry part={part} aborted={props.aborted} />
							);
						case "image":
							return (
								<ImagePartEntry
									part={part}
									aborted={props.aborted}
									showToast={props.showToast}
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
