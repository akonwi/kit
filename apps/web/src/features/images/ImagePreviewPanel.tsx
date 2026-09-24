import type {
	BoxRenderable,
	ImageRenderable,
	ScrollBoxRenderable,
} from "@opentui/core";
import { createEffect, createMemo, createSignal, Show } from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { Binding } from "../../shell/HintBar";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { scrollbarStyle, theme } from "../../shell/theme";
import {
	WorkspacePanelHeader,
	WorkspacePanelLayout,
} from "../../shell/WorkspacePanelLayout";
import { openImagePart } from "./open";
import type { ImagePreviewSource } from "./types";

const MIN_ZOOM = 1;
const MAX_ZOOM = 4;
const ZOOM_STEP = 0.5;
const PAN_STEP = 4;
const VIEWER_HINTS: Binding[] = [
	{ key: "+/-", action: "zoom" },
	{ key: "arrows", action: "pan" },
];

export function applyImagePreviewCanvasLayout(
	scroll: ScrollBoxRenderable | undefined,
	image: ImageRenderable | undefined,
	width: number,
	height: number,
): void {
	if (scroll && !scroll.isDestroyed) {
		scroll.content.minWidth = width;
		scroll.content.minHeight = height;
	}
	if (image && !image.isDestroyed) {
		image.width = width;
		image.height = height;
	}
}

export type ImagePreviewPanelProps = {
	image: ImagePreviewSource;
	active?: boolean;
	visible?: boolean;
	onClose: () => void;
	onFocusRequest?: () => void;
	onActionError: (error: unknown) => void;
};

export function ImagePreviewPanel(props: ImagePreviewPanelProps) {
	const [zoom, setZoom] = createSignal(1);
	const [viewportSize, setViewportSize] = createSignal({ width: 1, height: 1 });
	const [errorMessage, setErrorMessage] = createSignal<string>();
	const source = () => Uint8Array.from(Buffer.from(props.image.data, "base64"));
	let panelRef: BoxRenderable | undefined;
	let scrollRef: ScrollBoxRenderable | undefined;
	let imageRef: ImageRenderable | undefined;
	const active = () => props.active !== false;
	const visible = () => props.visible !== false;
	const imageWidth = createMemo(() =>
		Math.max(1, Math.floor(viewportSize().width * zoom())),
	);
	const imageHeight = createMemo(() =>
		Math.max(1, Math.floor(viewportSize().height * zoom())),
	);

	function syncCanvasLayout(): void {
		applyImagePreviewCanvasLayout(
			scrollRef,
			imageRef,
			imageWidth(),
			imageHeight(),
		);
	}

	createEffect(syncCanvasLayout);
	createEffect(() => {
		if (!active()) return;
		queueMicrotask(() => {
			if (panelRef && !panelRef.isDestroyed) panelRef.focus();
		});
	});

	function syncViewportSize(): void {
		if (!scrollRef) return;
		setViewportSize({
			width: Math.max(1, scrollRef.viewport.width || scrollRef.width - 1),
			height: Math.max(1, scrollRef.viewport.height || scrollRef.height - 1),
		});
	}

	function updateZoom(next: number): void {
		setZoom(Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, next)));
		queueMicrotask(() => {
			if (scrollRef && !scrollRef.isDestroyed) {
				scrollRef.scrollTo({ x: 0, y: 0 });
			}
		});
	}

	async function openExternally(): Promise<void> {
		const result = await openImagePart({
			type: "image",
			data: props.image.data,
			mimeType: props.image.mimeType,
			filename: props.image.filename,
			sourcePath: props.image.sourcePath,
		});
		if (!result.ok) props.onActionError(new Error(result.message));
	}

	useKeymapLayer(() => ({
		scope: "panel",
		when: active,
		diagnosticsWhen: active,
		commands: {
			"image-preview.close": props.onClose,
			"image-preview.zoom-in": () => updateZoom(zoom() + ZOOM_STEP),
			"image-preview.zoom-out": () => updateZoom(zoom() - ZOOM_STEP),
			"image-preview.reset-zoom": () => updateZoom(1),
			"image-preview.pan-up": () => scrollRef?.scrollBy({ x: 0, y: -PAN_STEP }),
			"image-preview.pan-down": () =>
				scrollRef?.scrollBy({ x: 0, y: PAN_STEP }),
			"image-preview.pan-left": () =>
				scrollRef?.scrollBy({ x: -PAN_STEP, y: 0 }),
			"image-preview.pan-right": () =>
				scrollRef?.scrollBy({ x: PAN_STEP, y: 0 }),
			...(props.image.sourcePath
				? { "image-preview.open-external": openExternally }
				: {}),
		},
	}));

	const dimensions = () =>
		props.image.width && props.image.height
			? `${props.image.width}×${props.image.height}`
			: null;

	return (
		<box
			ref={(value) => {
				panelRef = value;
			}}
			width="100%"
			height="100%"
			focusable
			onMouseDown={(event) => {
				if (event.button !== 0) return;
				panelRef?.focus();
				props.onFocusRequest?.();
			}}
		>
			<WorkspacePanelLayout
				header={
					<WorkspacePanelHeader
						left={
							<text fg={theme.textMuted}>
								{Math.round(zoom() * 100)}%
								{dimensions() ? ` · ${dimensions()}` : ""}
							</text>
						}
					/>
				}
				footer={
					<KeymapHintBar
						group="image-preview"
						prefixBindings={VIEWER_HINTS}
						borderless
					/>
				}
			>
				<Show
					when={!errorMessage()}
					fallback={
						<box
							flexGrow={1}
							alignItems="center"
							justifyContent="center"
							paddingX={1}
						>
							<text fg={theme.errorText}>{errorMessage()}</text>
						</box>
					}
				>
					<scrollbox
						ref={(value) => {
							scrollRef = value;
							syncCanvasLayout();
							queueMicrotask(syncViewportSize);
						}}
						flexGrow={1}
						scrollX
						scrollY
						contentOptions={{
							minWidth: imageWidth(),
							minHeight: imageHeight(),
						}}
						style={scrollbarStyle()}
						onSizeChange={syncViewportSize}
					>
						<Show when={visible()}>
							<image
								ref={(value) => {
									imageRef = value;
									syncCanvasLayout();
								}}
								source={source()}
								width={imageWidth()}
								height={imageHeight()}
								flexShrink={0}
								fit="fit"
								protocol="auto"
								onError={(error) => {
									setErrorMessage(
										error instanceof Error ? error.message : String(error),
									);
								}}
							/>
						</Show>
					</scrollbox>
				</Show>
			</WorkspacePanelLayout>
		</box>
	);
}
