import { createResource, For, Show } from "solid-js";
import type {
	OverlayComponentProps,
	OverlaySurfaceProps,
} from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { CodeReviewMessagePart } from "../../messages/parts";
import { Dialog } from "../../shell/Dialog";
import type { ReviewLine } from "../../shell/diff/types";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { scrollbarStyle, syntaxStyle, theme } from "../../shell/theme";
import {
	extractReviewRangeExcerpt,
	loadReviewAttachmentContext,
	type ReviewRangeExcerpt,
} from "./review-attachment-context";

export type ReviewAttachmentSource =
	| { kind: "draft"; attachmentId: string }
	| {
			kind: "historical";
			id: string;
			review: CodeReviewMessagePart["review"];
	  };

export function reviewAttachmentSourceEquals(
	a: ReviewAttachmentSource,
	b: ReviewAttachmentSource,
): boolean {
	if (a.kind === "draft" && b.kind === "draft") {
		return a.attachmentId === b.attachmentId;
	}
	if (a.kind === "historical" && b.kind === "historical") {
		return a.id === b.id;
	}
	return false;
}

function reviewCounts(review: CodeReviewMessagePart["review"]): {
	comments: number;
	files: number;
} {
	return {
		comments: review.files.reduce(
			(sum, file) =>
				sum + (file.fileComment.trim() ? 1 : 0) + file.ranges.length,
			0,
		),
		files: review.files.length,
	};
}

function countLabel(count: number, singular: string): string {
	return `${count} ${singular}${count === 1 ? "" : "s"}`;
}

export function reviewAttachmentMetaText(
	review: CodeReviewMessagePart["review"],
): string {
	const counts = reviewCounts(review);
	return `${countLabel(counts.comments, "comment")} · ${countLabel(counts.files, "file")}`;
}

function rangeLabel(
	range: CodeReviewMessagePart["review"]["files"][number]["ranges"][number],
): string {
	const prefix = range.side === "additions" ? "+" : "-";
	return range.startLine === range.endLine
		? `${prefix}L${range.startLine}`
		: `${prefix}L${range.startLine}-${range.endLine}`;
}

function excerptLineColor(line: ReviewLine): string {
	if (line.kind === "add") return theme.toolText;
	if (line.kind === "delete") return theme.errorText;
	return theme.textSecondary;
}

function excerptLineBg(line: ReviewLine): string | undefined {
	if (line.kind === "add") return theme.diffAddedBg;
	if (line.kind === "delete") return theme.diffRemovedBg;
	return undefined;
}

function ReviewRangeExcerptView(props: { excerpt: ReviewRangeExcerpt }) {
	return (
		<box flexDirection="column" width="100%">
			<Show when={props.excerpt.truncatedBefore}>
				<text fg={theme.textPlaceholder}>context omitted</text>
			</Show>
			<For each={props.excerpt.lines}>
				{(line) => {
					const prefix =
						line.kind === "add" ? "+" : line.kind === "delete" ? "-" : " ";
					const lineNumber =
						line.kind === "delete"
							? line.deletionLineNumber
							: (line.additionLineNumber ?? line.deletionLineNumber);
					return (
						<text
							fg={excerptLineColor(line)}
							bg={excerptLineBg(line)}
							wrapMode="none"
						>
							{prefix} {lineNumber ?? ""} {line.text}
						</text>
					);
				}}
			</For>
			<Show when={props.excerpt.truncatedAfter}>
				<text fg={theme.textPlaceholder}>context omitted</text>
			</Show>
		</box>
	);
}

function ReviewAttachmentContent(props: {
	review: CodeReviewMessagePart["review"];
	draft: boolean;
	cwd?: string;
}) {
	const [context] = createResource(
		() => ({ review: props.review, draft: props.draft, cwd: props.cwd }),
		loadReviewAttachmentContext,
	);
	const contextLabel = () => {
		if (context.loading) return "loading context";
		switch (context()?.kind) {
			case "exact":
				return "exact commit context";
			case "live":
				return "live context";
			default:
				return "source context not retained";
		}
	};

	return (
		<box flexDirection="column" gap={1} width="100%">
			<box flexDirection="column" paddingX={1}>
				<Show
					when={props.review.commit}
					fallback={<text fg={theme.textMuted}>working tree</text>}
				>
					{(commit) => (
						<text fg={theme.textMuted}>
							<span style={{ fg: theme.metaText }}>
								{commit().sha.slice(0, 7)}
							</span>{" "}
							{commit().subject}
						</text>
					)}
				</Show>
				<text fg={theme.textPlaceholder}>{contextLabel()}</text>
			</box>

			<For each={props.review.files}>
				{(file) => {
					const contextFile = () => context()?.files.get(file.path);
					return (
						<box flexDirection="column" gap={1} paddingX={1}>
							<box flexDirection="row" justifyContent="space-between">
								<text fg={theme.textPrimary}>{file.path}</text>
								<Show when={contextFile()}>
									{(resolvedFile) => (
										<text fg={theme.textMuted}>
											{resolvedFile().changeCount} changes
										</text>
									)}
								</Show>
							</box>

							<Show when={file.fileComment.trim()}>
								<box
									border={["left"]}
									borderColor={theme.attachmentText}
									paddingLeft={1}
								>
									<markdown
										content={file.fileComment}
										syntaxStyle={syntaxStyle()}
										conceal
										fg={theme.textSecondary}
									/>
								</box>
							</Show>

							<For each={file.ranges}>
								{(range) => (
									<box flexDirection="column" paddingLeft={1}>
										<text
											fg={
												range.side === "additions"
													? theme.toolText
													: theme.errorText
											}
										>
											{rangeLabel(range)}
										</text>
										<Show
											when={extractReviewRangeExcerpt(contextFile(), range)}
										>
											{(excerpt) => (
												<ReviewRangeExcerptView excerpt={excerpt()} />
											)}
										</Show>
										<markdown
											content={range.comment}
											syntaxStyle={syntaxStyle()}
											conceal
											fg={theme.textSecondary}
										/>
									</box>
								)}
							</For>
						</box>
					);
				}}
			</For>
		</box>
	);
}

export type ReviewAttachmentSidebarProps = {
	review: CodeReviewMessagePart["review"];
	draft: boolean;
	cwd?: string;
	onClose: () => void;
	onEdit?: () => void;
};

export function ReviewAttachmentSidebar(props: ReviewAttachmentSidebarProps) {
	useKeymapLayer(() => ({
		scope: "panel",
		commands: props.draft
			? {
					"review-draft.close": props.onClose,
					"review-draft.edit": () => props.onEdit?.(),
				}
			: { "review-attachment.close": props.onClose },
	}));

	return (
		<box
			flexDirection="column"
			width="100%"
			height="100%"
			border={["left"]}
			borderColor={theme.borderDefault}
			backgroundColor={theme.bg}
		>
			<box
				flexShrink={0}
				flexDirection="row"
				justifyContent="space-between"
				paddingX={1}
				border={["bottom"]}
				borderColor={theme.borderDefault}
			>
				<text fg={theme.textPrimary}>
					{props.draft ? "Code review draft" : "Code review"}
				</text>
				<text fg={theme.textMuted}>
					{reviewAttachmentMetaText(props.review)}
				</text>
			</box>

			<scrollbox flexGrow={1} scrollY style={scrollbarStyle()}>
				<ReviewAttachmentContent
					review={props.review}
					draft={props.draft}
					cwd={props.cwd}
				/>
			</scrollbox>

			<box flexShrink={0}>
				<KeymapHintBar
					group={props.draft ? "review-draft" : "review-attachment"}
				/>
			</box>
		</box>
	);
}

export type ReviewAttachmentDialogProps = OverlayComponentProps<void> & {
	review: CodeReviewMessagePart["review"];
	draft: boolean;
	cwd?: string;
	onEdit?: () => void;
	surfaceProps?: OverlaySurfaceProps;
};

export function ReviewAttachmentDialog(props: ReviewAttachmentDialogProps) {
	const close = () => props.done(undefined);
	const edit = () => {
		close();
		props.onEdit?.();
	};

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.active !== false,
		commands: props.draft
			? {
					"review-draft.close": close,
					"review-draft.edit": edit,
				}
			: { "review-attachment.close": close },
	}));

	return (
		<Dialog.Root
			width="90%"
			maxWidth={120}
			height="80%"
			surfaceProps={props.surfaceProps}
		>
			<Dialog.Header>
				<Dialog.Title>
					{props.draft ? "Code review draft" : "Code review"}
				</Dialog.Title>
				<Dialog.Meta>{reviewAttachmentMetaText(props.review)}</Dialog.Meta>
			</Dialog.Header>
			<Dialog.Body>
				<scrollbox flexGrow={1} scrollY style={scrollbarStyle()}>
					<ReviewAttachmentContent
						review={props.review}
						draft={props.draft}
						cwd={props.cwd}
					/>
				</scrollbox>
			</Dialog.Body>
			<Dialog.Footer>
				<KeymapHintBar
					borderless
					group={props.draft ? "review-draft" : "review-attachment"}
				/>
			</Dialog.Footer>
		</Dialog.Root>
	);
}
