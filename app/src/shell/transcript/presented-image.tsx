import { TextAttributes } from "@opentui/core";
import { createSignal, Show } from "solid-js";
import { openImagePart } from "../../features/images/open";
import type { ToolResultMessage } from "../../runtime/agent";
import { SHOW_IMAGE_TOOL_NAME, type ShowImageDetails } from "../../tools";
import { IMAGE, MIDDLE_DOT, TRIANGLE_DOWN, TRIANGLE_RIGHT } from "../glyphs";
import { theme } from "../theme";
import type { TranscriptToast } from "./types";

const IMAGE_PREVIEW_ROWS = 12;
const IMAGE_PREVIEW_MAX_COLUMNS = 72;
const ABORTED_ATTRS = TextAttributes.DIM | TextAttributes.STRIKETHROUGH;

export type PresentedToolImage = {
	data: string;
	mimeType: string;
	path: string;
	filename: string;
	caption?: string;
	width?: number;
	height?: number;
};

function isShowImageDetails(value: unknown): value is ShowImageDetails {
	if (!value || typeof value !== "object") return false;
	const details = value as Partial<ShowImageDetails>;
	return (
		details.presentation === "transcript-image" &&
		typeof details.path === "string" &&
		typeof details.filename === "string" &&
		typeof details.mimeType === "string"
	);
}

export function extractPresentedToolImage(
	toolName: string,
	result: unknown,
): PresentedToolImage | null {
	if (
		toolName !== SHOW_IMAGE_TOOL_NAME ||
		!result ||
		typeof result !== "object"
	) {
		return null;
	}
	const candidate = result as Partial<ToolResultMessage>;
	if (candidate.isError || !Array.isArray(candidate.content)) return null;
	if (!isShowImageDetails(candidate.details)) return null;
	const image = candidate.content.find(
		(block) =>
			block.type === "image" &&
			"data" in block &&
			typeof block.data === "string" &&
			typeof block.mimeType === "string",
	);
	if (!image || image.type !== "image") return null;
	return {
		data: image.data,
		mimeType: image.mimeType,
		path: candidate.details.path,
		filename: candidate.details.filename,
		...(candidate.details.caption
			? { caption: candidate.details.caption }
			: {}),
		...(typeof candidate.details.width === "number"
			? { width: candidate.details.width }
			: {}),
		...(typeof candidate.details.height === "number"
			? { height: candidate.details.height }
			: {}),
	};
}

function ImagePreview(props: {
	image: PresentedToolImage;
	onError: (error: unknown) => void;
	onOpen: () => void;
}) {
	const source = Uint8Array.from(Buffer.from(props.image.data, "base64"));
	return (
		<image
			source={source}
			width="100%"
			maxWidth={IMAGE_PREVIEW_MAX_COLUMNS}
			height={IMAGE_PREVIEW_ROWS}
			flexShrink={0}
			fit="fit"
			protocol="auto"
			onError={props.onError}
			onMouseUp={(event) => {
				if (event.button !== 0) return;
				event.stopPropagation();
				props.onOpen();
			}}
		/>
	);
}

export function PresentedImage(props: {
	image: PresentedToolImage;
	expanded: boolean;
	onExpandedChange: (expanded: boolean) => void;
	aborted?: boolean;
	showToast: (toast: TranscriptToast) => void;
}) {
	const [loadError, setLoadError] = createSignal(false);
	const dimensions = () =>
		props.image.width && props.image.height
			? `${props.image.width}×${props.image.height}`
			: null;

	function openOriginal() {
		void openImagePart({
			type: "image",
			data: props.image.data,
			mimeType: props.image.mimeType,
			filename: props.image.filename,
			sourcePath: props.image.path,
		}).then((result) => {
			if (result.ok) return;
			props.showToast({
				title: "Could not open image",
				subtitle: result.message,
				variant: "error",
			});
		});
	}

	return (
		<box flexDirection="column" gap={props.expanded ? 1 : 0} width="100%">
			<box
				width="100%"
				onMouseUp={(event) => {
					if (event.button !== 0 || props.aborted) return;
					event.stopPropagation();
					setLoadError(false);
					props.onExpandedChange(!props.expanded);
				}}
			>
				<text
					fg={props.aborted ? theme.textMuted : theme.attachmentText}
					attributes={props.aborted ? ABORTED_ATTRS : undefined}
				>
					{props.expanded ? TRIANGLE_DOWN : TRIANGLE_RIGHT} {IMAGE}{" "}
					{props.image.filename}
					<Show when={dimensions()}>
						{(value) => ` ${MIDDLE_DOT} ${value()}`}
					</Show>
					<Show when={loadError()}>{` ${MIDDLE_DOT} preview unavailable`}</Show>
				</text>
			</box>
			<text fg={theme.textSecondary} visible={Boolean(props.image.caption)}>
				{props.image.caption ?? ""}
			</text>
			<Show when={props.expanded} fallback={<box visible={false} />}>
				<ImagePreview
					image={props.image}
					onOpen={openOriginal}
					onError={(error) => {
						console.warn("[images] failed to render transcript image", {
							filename: props.image.filename,
							error,
						});
						setLoadError(true);
						props.onExpandedChange(false);
						props.showToast({
							title: "Could not preview image",
							subtitle: error instanceof Error ? error.message : String(error),
							variant: "error",
						});
					}}
				/>
			</Show>
		</box>
	);
}
