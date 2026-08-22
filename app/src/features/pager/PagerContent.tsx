import type { PasteEvent } from "@opentui/core";
import { createEffect, createSignal, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { Binding } from "../../shell/HintBar";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { MessageComposer } from "../../shell/MessageComposer";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { formatSelectionAsQuote } from "../../shell/selection";
import { theme } from "../../shell/theme";
import {
	PagerDocument,
	type PagerSelectionMenuController,
} from "./PagerDocument";
import type { PagerController } from "./pager-controller";

const EDIT_PREFIX_BINDINGS: Binding[] = [
	{ key: "Enter", action: "save" },
	{ key: "Shift+Enter", action: "newline" },
];

export function insertPagerSelectionQuote(options: {
	text: string;
	selection: string;
	cursorOffset: number;
}): { text: string; cursorOffset: number } {
	const cursorOffset = Math.max(
		0,
		Math.min(options.cursorOffset, options.text.length),
	);
	const quote = formatSelectionAsQuote(
		options.selection,
		cursorOffset > 0 && options.text[cursorOffset - 1] !== "\n",
	);
	if (!quote) return { text: options.text, cursorOffset };
	return {
		text: `${options.text.slice(0, cursorOffset)}${quote}${options.text.slice(cursorOffset)}`,
		cursorOffset: cursorOffset + quote.length,
	};
}

export type PagerContentProps = {
	pager: PagerController;
	onClose: () => void;
	copyText: (text: string) => Promise<void>;
	onCopyError: (error: unknown) => void;
	surfaceProps?: OverlaySurfaceProps;
};

export function PagerContent(props: PagerContentProps) {
	const pager = props.pager;
	const [mode, setMode] = createSignal<"navigate" | "edit">("navigate");
	const [noteText, setNoteText] = createSignal("");
	const [selectionMenuOpen, setSelectionMenuOpen] = createSignal(false);
	let scrollRef:
		| { scrollBy: (opts: { x: number; y: number }) => void }
		| undefined;
	let textareaRef:
		| {
				plainText: string;
				cursorOffset: number;
				setText: (value: string) => void;
		  }
		| undefined;
	let selectionMenuController: PagerSelectionMenuController | undefined;

	function bindScroll(ref: typeof scrollRef) {
		scrollRef = ref;
		pager.setScrollDelegate({
			scrollBy: (delta: number) => scrollRef?.scrollBy({ x: 0, y: delta }),
		});
	}

	createEffect(() => {
		const saved = pager.notes.get(pager.currentIndex) ?? "";
		setNoteText(saved);
		try {
			textareaRef?.setText(saved);
		} catch {
			textareaRef = undefined;
		}
	});

	createEffect(() => {
		if (!pager.active) {
			setMode("navigate");
			setNoteText("");
		}
	});

	const noteCount = () => pager.getNoteCount();
	const currentNote = () => pager.notes.get(pager.currentIndex) ?? "";
	const noteEditorFocused = () => pager.active && mode() === "edit";

	function enterEditMode() {
		setNoteText(currentNote());
		setMode("edit");
	}

	function saveNote() {
		pager.setNote(pager.currentIndex, noteText());
		setMode("navigate");
	}

	function cancelEdit() {
		const saved = currentNote();
		setNoteText(saved);
		textareaRef?.setText(saved);
		setMode("navigate");
	}

	function clearCurrentNote() {
		pager.setNote(pager.currentIndex, "");
	}

	function quoteSelectedText(selection: string): void {
		const editing = mode() === "edit";
		const current = editing
			? (textareaRef?.plainText ?? noteText())
			: currentNote();
		const insertion = insertPagerSelectionQuote({
			text: current,
			selection,
			cursorOffset: editing
				? (textareaRef?.cursorOffset ?? current.length)
				: current.length,
		});
		setNoteText(insertion.text);
		setMode("edit");
		queueMicrotask(() => {
			if (!textareaRef) return;
			textareaRef.setText(insertion.text);
			textareaRef.cursorOffset = insertion.cursorOffset;
		});
	}

	function closePager() {
		pager.closeView();
		props.onClose();
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => pager.active && mode() === "navigate" && !selectionMenuOpen(),
		commands: {
			"pager.previous-section": pager.prevSection,
			"pager.next-section": pager.nextSection,
			"pager.scroll-up": pager.scrollUp,
			"pager.scroll-down": pager.scrollDown,
			"pager.edit-note": enterEditMode,
			"pager.close": closePager,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: () =>
			pager.active &&
			mode() === "navigate" &&
			!selectionMenuOpen() &&
			currentNote().length > 0,
		commands: {
			"pager.clear-note": clearCurrentNote,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: () =>
			pager.active &&
			mode() === "navigate" &&
			!selectionMenuOpen() &&
			noteCount() > 0,
		commands: {
			"pager.submit-feedback": closePager,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => pager.active && mode() === "edit" && !selectionMenuOpen(),
		commands: {
			"pager.back": cancelEdit,
		},
	}));

	function handlePaste(event: PasteEvent) {
		if (mode() !== "edit") return;
		const pasted = new TextDecoder()
			.decode(event.bytes)
			.replace(/\r\n/g, "\n")
			.replace(/\r/g, "\n");
		setNoteText((current) => `${current}${pasted}`);
	}

	const sectionPct = () => {
		const total = pager.sections.length;
		if (total <= 1) return 100;
		return Math.round(((pager.currentIndex + 1) / total) * 100);
	};

	return (
		<Show when={pager.active}>
			<ScreenLayout
				surfaceProps={props.surfaceProps}
				onMouseDown={(event) => {
					event.stopPropagation();
					selectionMenuController?.close();
				}}
				onSizeChange={() => selectionMenuController?.close()}
				header={
					<ScreenHeader
						variant="strip"
						left={
							<text fg={theme.textPrimary}>
								<b>{pager.title}</b>
							</text>
						}
						right={
							<text fg={theme.textMuted}>
								{pager.currentIndex + 1}/{pager.sections.length}
								{noteCount() > 0
									? ` · ${noteCount()} note${noteCount() === 1 ? "" : "s"}`
									: ""}
							</text>
						}
						progress={sectionPct()}
					/>
				}
				footer={
					<box border={["top"]} borderColor={theme.borderDefault} paddingX={1}>
						<KeymapHintBar
							group="pager"
							borderless
							prefixBindings={mode() === "edit" ? EDIT_PREFIX_BINDINGS : []}
						/>
					</box>
				}
			>
				<PagerDocument
					sectionTitle={pager.currentSection?.sectionTitle ?? ""}
					body={pager.currentSection?.body ?? ""}
					zIndex={props.surfaceProps?.zIndex ?? 0}
					bindScroll={bindScroll}
					copyText={props.copyText}
					onCopyError={props.onCopyError}
					onQuote={quoteSelectedText}
					onMenuVisibilityChange={setSelectionMenuOpen}
					registerSelectionMenu={(controller) => {
						selectionMenuController = controller;
					}}
				/>
				<box
					flexShrink={0}
					border={["top"]}
					borderColor={
						noteEditorFocused() ? theme.borderAccent : theme.borderDefault
					}
					paddingX={1}
				>
					<Show
						when={mode() === "edit"}
						fallback={
							<Show
								when={currentNote()}
								fallback={
									<MessageComposer
										variant="dock"
										placeholder="Type your note..."
										focused={false}
										showCursor={false}
									/>
								}
							>
								<box flexDirection="column" maxHeight={5} overflow="hidden">
									<text fg={theme.reviewText}>
										<b>Note</b>
									</text>
									<text fg={theme.textSecondary}>{currentNote()}</text>
								</box>
							</Show>
						}
					>
						<MessageComposer
							variant="dock"
							ref={(element) => {
								textareaRef = element as typeof textareaRef;
							}}
							initialValue={noteText()}
							placeholder="Type your note..."
							focused={noteEditorFocused()}
							showCursor={noteEditorFocused()}
							maxHeight={6}
							keyBindings={[
								{ name: "return", action: "submit" },
								{ name: "return", shift: true, action: "newline" },
							]}
							onContentChange={() => setNoteText(textareaRef?.plainText ?? "")}
							onPaste={handlePaste}
							onSubmit={saveNote}
						/>
					</Show>
				</box>
			</ScreenLayout>
		</Show>
	);
}
