import type {
	BoxRenderable,
	ImageRenderable,
	ScrollBoxRenderable,
} from "@opentui/core";
import {
	createEffect,
	createMemo,
	createSignal,
	onCleanup,
	Show,
} from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { Binding } from "../../shell/HintBar";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { Spinner } from "../../shell/Spinner";
import { scrollbarStyle, theme } from "../../shell/theme";
import { WorkspacePanelLayout } from "../../shell/WorkspacePanelLayout";
import { openMermaidPreviewExternally } from "./external";
import { loadMermaidPreview } from "./load";
import type { MermaidPreviewImage } from "./render";

export const MERMAID_PREVIEW_MIN_COLS = 48;

const MIN_ZOOM = 1;
const MAX_ZOOM = 4;
const ZOOM_STEP = 0.5;
const PAN_STEP = 4;
const VIEWER_HINTS: Binding[] = [
	{ key: "+/-", action: "zoom" },
	{ key: "arrows", action: "pan" },
];

type PreviewState =
	| { status: "loading" }
	| { status: "ready"; image: MermaidPreviewImage }
	| { status: "error"; message: string };

export function applyMermaidPreviewCanvasLayout(
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

export type MermaidPreviewPanelProps = {
	source: string;
	active?: boolean;
	onClose: () => void;
	onFocusRequest?: () => void;
	onActionError: (error: unknown) => void;
};

export function MermaidPreviewPanel(props: MermaidPreviewPanelProps) {
	const [state, setState] = createSignal<PreviewState>({ status: "loading" });
	const [zoom, setZoom] = createSignal(1);
	const [viewportSize, setViewportSize] = createSignal({ width: 1, height: 1 });
	let panelRef: BoxRenderable | undefined;
	let scrollRef: ScrollBoxRenderable | undefined;
	let imageRef: ImageRenderable | undefined;
	const active = () => props.active !== false;
	const readyImage = () => {
		const current = state();
		return current.status === "ready" ? current.image : undefined;
	};
	const errorMessage = () => {
		const current = state();
		return current.status === "error" ? current.message : undefined;
	};
	const imageWidth = createMemo(() =>
		Math.max(1, Math.floor(viewportSize().width * zoom())),
	);
	const imageHeight = createMemo(() =>
		Math.max(1, Math.floor(viewportSize().height * zoom())),
	);

	createEffect(() => {
		const controller = new AbortController();
		setState({ status: "loading" });
		void loadMermaidPreview(
			props.source,
			{
				background: theme.bg,
				surface: theme.bgSurface,
				text: theme.textPrimary,
				mutedText: theme.textMuted,
				border: theme.borderDefault,
				line: theme.textSecondary,
			},
			controller.signal,
		)
			.then((image) => {
				if (!controller.signal.aborted) setState({ status: "ready", image });
			})
			.catch((error) => {
				if (controller.signal.aborted) return;
				setState({
					status: "error",
					message: error instanceof Error ? error.message : String(error),
				});
			});
		onCleanup(() => controller.abort());
	});

	function syncCanvasLayout(): void {
		applyMermaidPreviewCanvasLayout(
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
		const current = state();
		if (current.status !== "ready") return;
		try {
			await openMermaidPreviewExternally(props.source, current.image.png);
		} catch (error) {
			props.onActionError(error);
		}
	}

	useKeymapLayer(() => ({
		scope: "panel",
		when: active,
		diagnosticsWhen: active,
		commands: {
			"mermaid-preview.close": props.onClose,
			"mermaid-preview.zoom-in": () => updateZoom(zoom() + ZOOM_STEP),
			"mermaid-preview.zoom-out": () => updateZoom(zoom() - ZOOM_STEP),
			"mermaid-preview.reset-zoom": () => updateZoom(1),
			"mermaid-preview.pan-up": () => {
				if (!scrollRef?.isDestroyed) {
					scrollRef?.scrollBy({ x: 0, y: -PAN_STEP });
				}
			},
			"mermaid-preview.pan-down": () => {
				if (!scrollRef?.isDestroyed) {
					scrollRef?.scrollBy({ x: 0, y: PAN_STEP });
				}
			},
			"mermaid-preview.pan-left": () => {
				if (!scrollRef?.isDestroyed) {
					scrollRef?.scrollBy({ x: -PAN_STEP, y: 0 });
				}
			},
			"mermaid-preview.pan-right": () => {
				if (!scrollRef?.isDestroyed) {
					scrollRef?.scrollBy({ x: PAN_STEP, y: 0 });
				}
			},
			...(state().status === "ready"
				? { "mermaid-preview.open-external": openExternally }
				: {}),
		},
	}));

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
					<box
						flexShrink={0}
						paddingX={1}
						border={["bottom"]}
						borderColor={theme.borderDefault}
						flexDirection="row"
						justifyContent="space-between"
					>
						<text fg={theme.textPrimary}>Mermaid diagram</text>
						<text fg={theme.textMuted}>{Math.round(zoom() * 100)}%</text>
					</box>
				}
				footer={
					<KeymapHintBar
						group="mermaid-preview"
						prefixBindings={VIEWER_HINTS}
						borderless
					/>
				}
			>
				<Show when={state().status === "loading"}>
					<box flexGrow={1} alignItems="center" justifyContent="center">
						<Spinner fg={theme.metaText} />
						<text fg={theme.textSecondary}> Rendering diagram</text>
					</box>
				</Show>
				<Show when={errorMessage()}>
					{(message) => (
						<box
							flexGrow={1}
							alignItems="center"
							justifyContent="center"
							paddingX={1}
						>
							<text fg={theme.errorText}>{message()}</text>
						</box>
					)}
				</Show>
				<Show when={state().status === "ready"}>
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
						<image
							ref={(value) => {
								imageRef = value;
								syncCanvasLayout();
							}}
							source={readyImage()?.png}
							width={imageWidth()}
							height={imageHeight()}
							flexShrink={0}
							fit="fit"
							protocol="auto"
							onError={(error) => {
								setState({
									status: "error",
									message:
										error instanceof Error ? error.message : String(error),
								});
							}}
						/>
					</scrollbox>
				</Show>
			</WorkspacePanelLayout>
		</box>
	);
}
