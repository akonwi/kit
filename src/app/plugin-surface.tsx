import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import {
	createEffect,
	createMemo,
	createSignal,
	For,
	type JSX,
	Show,
} from "solid-js";
import type {
	PluginSurfaceChild,
	PluginSurfaceContainerNode,
	PluginSurfaceContext,
	PluginSurfaceFactory,
	PluginSurfaceInputNode,
	PluginSurfaceKeyEvent,
	PluginSurfaceListNode,
	PluginSurfaceNode,
	PluginSurfaceScrollNode,
	PluginSurfaceTextareaNode,
	PluginSurfaceTone,
	PluginSurfaceUI,
} from "../plugins/types";
import { Dialog } from "../shell/Dialog";
import { CHEVRON_RIGHT } from "../shell/glyphs";
import { HintBar } from "../shell/HintBar";
import { MessageComposer, type TextareaRef } from "../shell/MessageComposer";
import { ScreenHeader } from "../shell/ScreenHeader";
import { ScreenLayout } from "../shell/ScreenLayout";
import { syntaxStyle, theme } from "../shell/theme";
import type { OverlayComponentProps } from "./overlay-ui";

const DEFAULT_LIST_MAX_VISIBLE = 8;

export type PluginSurfaceOverlayProps<T> = OverlayComponentProps<T> & {
	factory: PluginSurfaceFactory<T>;
};

export function createPluginSurfaceUI(): PluginSurfaceUI {
	return {
		text: (text, options) => ({ type: "text", text, ...options }),
		markdown: (content, options) => ({ type: "markdown", content, ...options }),
		row: (children, options) => ({ type: "row", children, ...options }),
		column: (children, options) => ({ type: "column", children, ...options }),
		box: (children, options) => ({ type: "box", children, ...options }),
		scroll: (child, options) => ({ type: "scroll", child, ...options }),
		hintBar: (bindings, options) => ({ type: "hintBar", bindings, ...options }),
		list: (items, options) => ({ type: "list", items, ...options }),
		input: (options) => ({ type: "input", ...options }),
		textarea: (options) => ({ type: "textarea", ...options }),
		dialog: (options) => ({ type: "dialog", ...options }),
		screen: (options) => ({ type: "screen", ...options }),
	};
}

export function PluginSurfaceOverlay<T>(props: PluginSurfaceOverlayProps<T>) {
	const [version, setVersion] = createSignal(0);
	const [fatalError, setFatalError] = createSignal<string | undefined>();
	let closed = false;
	let renderFailed = false;

	const ctx: PluginSurfaceContext<T> = {
		get active() {
			return props.active;
		},
		invalidate() {
			setVersion((current) => current + 1);
		},
		close(result: T) {
			if (closed) return;
			closed = true;
			props.done(result);
		},
	};

	let surface: ReturnType<PluginSurfaceFactory<T>> | undefined;
	let initialError: string | undefined;
	try {
		surface = props.factory(ctx, createPluginSurfaceUI());
	} catch (cause) {
		initialError = formatPluginSurfaceError(cause);
	}

	const root = createMemo(() => {
		version();
		const currentError = fatalError() ?? initialError;
		if (currentError) return createPluginSurfaceErrorNode(currentError);
		if (!surface)
			return createPluginSurfaceErrorNode("Surface did not initialize.");
		try {
			const node = surface.render();
			renderFailed = false;
			return node;
		} catch (cause) {
			renderFailed = true;
			return createPluginSurfaceErrorNode(formatPluginSurfaceError(cause));
		}
	});

	useKeyboard((event: KeyEvent) => {
		if (!props.active) return;
		if (!surface || fatalError() || initialError || renderFailed) {
			if (event.name === "escape" || event.name === "return") {
				event.preventDefault();
				ctx.close(undefined as T);
			}
			return;
		}

		try {
			const result = surface.onKey?.(toPluginKeyEvent(event), ctx);
			if (result) {
				void result.catch((cause) => {
					setFatalError(formatPluginSurfaceError(cause));
				});
			}
		} catch (cause) {
			setFatalError(formatPluginSurfaceError(cause));
		}
	});

	return (
		<SurfaceRoot
			node={root()}
			ctx={ctx as PluginSurfaceContext<unknown>}
			surfaceProps={props.surfaceProps}
		/>
	);
}

function toPluginKeyEvent(event: KeyEvent): PluginSurfaceKeyEvent {
	return {
		name: event.name ?? "",
		ctrl: event.ctrl ?? false,
		shift: event.shift ?? false,
		alt: event.meta ?? false,
		meta: event.meta ?? false,
		option: (event as { option?: boolean }).option ?? false,
		preventDefault: () => event.preventDefault(),
	};
}

function formatPluginSurfaceError(cause: unknown): string {
	return cause instanceof Error ? cause.message : String(cause);
}

function createPluginSurfaceErrorNode(message: string): PluginSurfaceNode {
	return {
		type: "dialog",
		title: "Plugin surface crashed",
		body: {
			type: "column",
			gap: 1,
			children: [
				{
					type: "text",
					text: message,
					tone: "error",
				},
				{
					type: "text",
					text: "Press Esc to close this surface.",
					tone: "muted",
				},
			],
		},
		footer: {
			type: "hintBar",
			bindings: [{ key: "Esc", action: "close" }],
		},
	};
}

function toneColor(tone: PluginSurfaceTone | undefined): string {
	switch (tone) {
		case "secondary":
			return theme.textSecondary;
		case "muted":
			return theme.textMuted;
		case "placeholder":
			return theme.textPlaceholder;
		case "accent":
			return theme.borderAccent;
		case "success":
			return theme.toolText;
		case "warning":
			return theme.warningText;
		case "error":
			return theme.errorText;
		default:
			return theme.textPrimary;
	}
}

function renderChild(
	child: PluginSurfaceChild,
	ctx: PluginSurfaceContext<unknown>,
): JSX.Element {
	if (!child) return null;
	if (typeof child === "string") {
		return <text fg={theme.textPrimary}>{child}</text>;
	}
	return renderNode(child, ctx);
}

function SurfaceRoot(props: {
	node: PluginSurfaceNode;
	ctx: PluginSurfaceContext<unknown>;
	surfaceProps: OverlayComponentProps<unknown>["surfaceProps"];
}) {
	const rendered = createMemo(() => {
		const node = props.node;
		if (node.type === "dialog") {
			return (
				<Dialog.Root
					surfaceProps={props.surfaceProps}
					width={node.width ?? "70%"}
					maxWidth={node.maxWidth ?? 96}
					minWidth={node.minWidth ?? 48}
					height={node.height}
					padding={0}
				>
					<box flexGrow={1} flexDirection="column" paddingX={1}>
						<Dialog.Header>
							<Dialog.Title>{node.title}</Dialog.Title>
						</Dialog.Header>
						<box flexGrow={1} flexDirection="column" overflow="hidden">
							{renderChild(node.body, props.ctx)}
						</box>
						<Show when={node.footer}>
							{(footer) => (
								<Dialog.Footer paddingY={1}>
									{renderChild(footer(), props.ctx)}
								</Dialog.Footer>
							)}
						</Show>
					</box>
				</Dialog.Root>
			);
		}

		if (node.type === "screen") {
			return (
				<ScreenLayout
					surfaceProps={props.surfaceProps}
					header={
						<ScreenHeader
							left={<text fg={theme.textPrimary}>{node.title}</text>}
							right={
								node.right ? (
									<text fg={theme.textMuted}>{node.right}</text>
								) : undefined
							}
							progress={node.progress}
						/>
					}
					footer={renderChild(node.footer, props.ctx)}
				>
					{renderChild(node.body, props.ctx)}
				</ScreenLayout>
			);
		}

		return renderNode(node, props.ctx);
	});

	return <>{rendered()}</>;
}

function renderNode(
	node: PluginSurfaceNode,
	ctx: PluginSurfaceContext<unknown>,
): JSX.Element {
	switch (node.type) {
		case "text":
			return (
				<text fg={toneColor(node.tone)}>
					{node.bold ? <b>{node.text}</b> : node.text}
				</text>
			);
		case "markdown":
			return (
				<markdown
					content={node.content}
					syntaxStyle={syntaxStyle()}
					conceal
					fg={toneColor(node.tone)}
				/>
			);
		case "row":
		case "column":
		case "box":
			return <SurfaceContainer node={node} ctx={ctx} />;
		case "scroll":
			return <SurfaceScroll node={node} ctx={ctx} />;
		case "hintBar":
			return (
				<HintBar
					bindings={node.bindings}
					borderless={node.borderless ?? true}
				/>
			);
		case "list":
			return <SurfaceList node={node} />;
		case "input":
			return <SurfaceInput node={node} ctx={ctx} />;
		case "textarea":
			return <SurfaceTextarea node={node} ctx={ctx} />;
		case "dialog":
		case "screen":
			return <SurfaceRoot node={node} ctx={ctx} surfaceProps={{}} />;
	}
}

function SurfaceContainer(props: {
	node: PluginSurfaceContainerNode;
	ctx: PluginSurfaceContext<unknown>;
}) {
	const flexDirection = () => (props.node.type === "row" ? "row" : "column");
	return (
		<box
			flexDirection={flexDirection()}
			gap={props.node.gap}
			flexGrow={props.node.grow ? 1 : undefined}
			flexShrink={props.node.shrink === false ? 0 : undefined}
			width={props.node.width}
			height={props.node.height}
			padding={props.node.padding}
			paddingX={props.node.paddingX}
			paddingY={props.node.paddingY}
			overflow={props.node.overflow === "hidden" ? "hidden" : undefined}
		>
			<For each={props.node.children}>
				{(child) => renderChild(child, props.ctx)}
			</For>
		</box>
	);
}

function SurfaceScroll(props: {
	node: PluginSurfaceScrollNode;
	ctx: PluginSurfaceContext<unknown>;
}) {
	return (
		<scrollbox
			flexGrow={1}
			scrollY
			paddingX={props.node.paddingX}
			paddingY={props.node.paddingY}
			style={{
				scrollbarOptions: {
					trackOptions: {
						foregroundColor: theme.scrollbarFg,
						backgroundColor: theme.scrollbarBg,
					},
				},
			}}
		>
			<box flexDirection="column" width="100%">
				{renderChild(props.node.child, props.ctx)}
			</box>
		</scrollbox>
	);
}

function SurfaceList(props: { node: PluginSurfaceListNode }) {
	const selectedIndex = createMemo(() =>
		Math.max(
			0,
			Math.min(
				props.node.selectedIndex ?? 0,
				Math.max(0, props.node.items.length - 1),
			),
		),
	);
	const visibleItems = createMemo(() => {
		const maxVisible = props.node.maxVisible ?? DEFAULT_LIST_MAX_VISIBLE;
		const items = props.node.items;
		const selectedIndexValue = selectedIndex();
		if (items.length <= maxVisible) {
			return items.map((item, index) => ({ item, index }));
		}
		let offset = selectedIndexValue - Math.floor(maxVisible / 2);
		offset = Math.max(0, Math.min(offset, items.length - maxVisible));
		return items
			.slice(offset, offset + maxVisible)
			.map((item, index) => ({ item, index: offset + index }));
	});

	return (
		<box flexDirection="column" overflow="hidden">
			<For each={visibleItems()}>
				{(entry) => {
					const isFocused = () => entry.index === selectedIndex();
					const fg = () =>
						isFocused() ? theme.pickerFocusedText : theme.textPrimary;
					const bg = () =>
						isFocused() ? theme.pickerFocusedBg : theme.bgTransparent;
					return (
						<box
							flexDirection="row"
							width="100%"
							height={1}
							overflow="hidden"
							gap={1}
							backgroundColor={bg()}
						>
							<text fg={fg()} bg={bg()}>
								{isFocused() ? `${CHEVRON_RIGHT} ` : "  "}
								{entry.item.label}
							</text>
							<Show when={entry.item.description}>
								{(description) => (
									<>
										<box flexGrow={1} />
										<text fg={fg()} bg={bg()}>
											{description()}
										</text>
									</>
								)}
							</Show>
						</box>
					);
				}}
			</For>
		</box>
	);
}

function SurfaceInput(props: {
	node: PluginSurfaceInputNode;
	ctx: PluginSurfaceContext<unknown>;
}) {
	return (
		<box border borderColor={theme.borderAccent} paddingX={1} width="100%">
			<input
				flexGrow={1}
				focused={props.ctx.active && props.node.focused !== false}
				value={props.node.value}
				placeholder={props.node.placeholder ?? ""}
				placeholderColor={theme.textPlaceholder}
				onInput={(value: string) => {
					props.node.onInput?.(value);
					props.ctx.invalidate();
				}}
			/>
		</box>
	);
}

function SurfaceTextarea(props: {
	node: PluginSurfaceTextareaNode;
	ctx: PluginSurfaceContext<unknown>;
}) {
	let textareaRef: TextareaRef | undefined;

	createEffect(() => {
		const value = props.node.value;
		if (textareaRef && textareaRef.plainText !== value) {
			textareaRef.setText(value);
		}
	});

	return (
		<MessageComposer
			ref={(ref) => {
				textareaRef = ref;
				if (textareaRef && textareaRef.plainText !== props.node.value) {
					textareaRef.setText(props.node.value);
				}
			}}
			initialValue={props.node.value}
			placeholder={props.node.placeholder}
			focused={props.ctx.active && props.node.focused !== false}
			maxHeight={props.node.maxHeight}
			keyBindings={[{ name: "return", shift: true, action: "newline" }]}
			onContentChange={() => {
				props.node.onInput?.(textareaRef?.plainText ?? "");
				props.ctx.invalidate();
			}}
		/>
	);
}
