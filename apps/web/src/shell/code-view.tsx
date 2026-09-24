import type { BoxRenderable } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { type Component, createMemo, createSignal, For, Show } from "solid-js";
import { estimateWrappedRows } from "./diff/ReviewDiffModel";
import { inferFiletype } from "./filetype";
import { syntaxStyle, theme } from "./theme";

type ScrollableTextRenderable = {
	scrollX: number;
	scrollY: number;
};

function resetLineScroll(ref: ScrollableTextRenderable | undefined) {
	queueMicrotask(() => {
		if (!ref) return;
		ref.scrollX = 0;
		ref.scrollY = 0;
	});
}

export type CodeViewProps = {
	/** File path used to infer the filetype when `filetype` is not provided. */
	path?: string;
	/** Explicit filetype override (e.g. "typescript", "markdown"). */
	filetype?: string;
	/** File content to render. */
	content: string;
	/** 1-based line number for the first line. Defaults to 1. */
	startLine?: number;
	/**
	 * Whether to render the line-number gutter. Defaults to true. Callers
	 * that don't have reliable absolute line numbers should pass `false`
	 * rather than showing potentially misleading values.
	 */
	showLineNumbers?: boolean;
};

/**
 * Read-only file content display with per-line syntax highlighting and an
 * optional line-number gutter (see `showLineNumbers`). Mirrors the visual
 * layout of the code review's read-only file viewer (right-aligned muted
 * line numbers, two-space separator, syntax-highlighted content) without
 * any of the interactive machinery (scroll, cursor, annotations).
 */
export const CodeView: Component<CodeViewProps> = (props) => {
	const renderer = useRenderer();
	let rootRef: BoxRenderable | undefined;
	const [rootWidth, setRootWidth] = createSignal(renderer.width);
	const filetype = createMemo(
		() =>
			props.filetype ?? (props.path ? inferFiletype(props.path) : undefined),
	);
	const lines = createMemo(() => {
		const normalized = props.content.replace(/\r\n/g, "\n");
		const out = normalized.split("\n");
		// Drop the trailing empty entry left by a terminating newline so the
		// rendered line count matches the file's logical line count.
		if (out.length > 0 && out[out.length - 1] === "") out.pop();
		return out;
	});
	const startLine = createMemo(() => props.startLine ?? 1);
	const lineNumberWidth = createMemo(() => {
		const last = startLine() + Math.max(0, lines().length - 1);
		return Math.max(1, String(last).length);
	});
	const showLineNumbers = createMemo(() => props.showLineNumbers !== false);
	const contentWidth = createMemo(() =>
		Math.max(1, rootWidth() - (showLineNumbers() ? lineNumberWidth() + 2 : 0)),
	);
	const totalRows = createMemo(() =>
		Math.max(
			1,
			lines().reduce(
				(total, line) => total + estimateWrappedRows(line, contentWidth()),
				0,
			),
		),
	);
	return (
		<box
			ref={(value) => {
				rootRef = value;
				setRootWidth(Math.max(1, rootRef.width));
			}}
			onSizeChange={() => {
				if (rootRef) setRootWidth(Math.max(1, rootRef.width));
			}}
			flexDirection="column"
			gap={0}
			width="100%"
			height={totalRows()}
			flexShrink={0}
		>
			<For each={lines()}>
				{(line, idx) => {
					let contentRef: ScrollableTextRenderable | undefined;
					const lineNum = () => startLine() + idx();
					const lineHeight = () =>
						Math.max(1, estimateWrappedRows(line, contentWidth()));
					return (
						<box
							flexDirection="row"
							alignItems="flex-start"
							height={lineHeight()}
							flexShrink={0}
							width="100%"
						>
							<Show when={showLineNumbers()}>
								<text fg={theme.textMuted} flexShrink={0} height={1}>
									{String(lineNum()).padStart(lineNumberWidth())}
								</text>
								<text fg={theme.textMuted} flexShrink={0} height={1}>
									{"  "}
								</text>
							</Show>
							<Show
								when={filetype()}
								fallback={
									<text
										ref={(value) => {
											contentRef = value as ScrollableTextRenderable;
										}}
										fg={theme.textPrimary}
										flexGrow={1}
										height={lineHeight()}
										wrapMode="word"
										onMouseScroll={() => resetLineScroll(contentRef)}
									>
										{line}
									</text>
								}
							>
								{(ft) => (
									<code
										ref={(value) => {
											contentRef = value as ScrollableTextRenderable;
										}}
										content={line}
										filetype={ft()}
										syntaxStyle={syntaxStyle()}
										conceal={false}
										flexGrow={1}
										height={lineHeight()}
										wrapMode="word"
										onMouseScroll={() => resetLineScroll(contentRef)}
									/>
								)}
							</Show>
						</box>
					);
				}}
			</For>
		</box>
	);
};
