import type { KeyEvent, PasteEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { createEffect, createSignal, For, Show } from "solid-js";
import type { PagerController } from "../features/pager";
import { syntaxStyle, theme } from "./theme";

export type PagerModalProps = {
	pager: PagerController;
};

export function PagerModal(props: PagerModalProps) {
	const pager = props.pager;

	// Local UI state
	const [mode, setMode] = createSignal<"navigate" | "edit">("navigate");
	const [noteText, setNoteText] = createSignal("");
	let scrollRef:
		| { scrollBy: (opts: { x: number; y: number }) => void }
		| undefined;
	let textareaRef:
		| { plainText: string; setText: (v: string) => void }
		| undefined;

	// Bind scroll delegate so controller keyboard helpers work
	function bindScroll(ref: typeof scrollRef) {
		scrollRef = ref;
		pager.setScrollDelegate({
			scrollBy: (delta: number) => scrollRef?.scrollBy({ x: 0, y: delta }),
		});
	}

	// When the section changes, load its saved note into the edit textarea
	createEffect(() => {
		const idx = pager.currentIndex;
		const saved = pager.notes.get(idx) ?? "";
		setNoteText(saved);
		try {
			textareaRef?.setText(saved);
		} catch {
			textareaRef = undefined;
		}
	});

	// When the pager closes, reset local mode
	createEffect(() => {
		if (!pager.active) {
			setMode("navigate");
			setNoteText("");
		}
	});

	function saveNote() {
		pager.setNote(pager.currentIndex, noteText());
	}

	function enterEditMode() {
		setMode("edit");
	}

	function exitEditMode() {
		saveNote();
		setMode("navigate");
	}

	async function handleSubmit() {
		saveNote();
		await pager.submitFeedback();
	}

	// Global keyboard handler (navigate mode + edit-mode escape/submit)
	useKeyboard((e: KeyEvent) => {
		if (!pager.active) return;

		if (mode() === "edit") {
			if (e.name === "escape") {
				e.preventDefault();
				exitEditMode();
			} else if (e.ctrl && e.name === "return") {
				e.preventDefault();
				handleSubmit();
			}
			return;
		}

		// Navigate mode
		if (e.name === "escape" || e.name === "q") {
			e.preventDefault();
			pager.close();
			return;
		}
		if (e.name === "n" || e.name === "i") {
			e.preventDefault();
			enterEditMode();
			return;
		}
		if (e.ctrl && e.name === "return") {
			e.preventDefault();
			handleSubmit();
			return;
		}
		if (e.name === "left" || e.name === "h") {
			e.preventDefault();
			pager.prevSection();
			return;
		}
		if (e.name === "right" || e.name === "l") {
			e.preventDefault();
			pager.nextSection();
			return;
		}
		if (e.name === "up" || e.name === "k") {
			e.preventDefault();
			pager.scrollUp();
			return;
		}
		if (e.name === "down" || e.name === "j") {
			e.preventDefault();
			pager.scrollDown();
			return;
		}
	});

	function handlePaste(event: PasteEvent) {
		if (mode() !== "edit") return;
		const pasted = event.text.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
		setNoteText((cur) => `${cur}${pasted}`);
	}

	const noteCount = () => pager.getNoteCount();
	const currentNote = () => pager.notes.get(pager.currentIndex) ?? "";

	return (
		<Show when={pager.active}>
			<box
				position="absolute"
				top={0}
				left={0}
				width="100%"
				height="100%"
				zIndex={1200}
				backgroundColor={theme.bg}
				flexDirection="column"
				border
				borderColor={theme.borderFocused}
			>
				{/* Header: title + position */}
				<box
					flexShrink={0}
					flexDirection="row"
					justifyContent="space-between"
					paddingX={1}
					backgroundColor={theme.bgSurface}
				>
					<text fg={theme.textPrimary}>
						<b>{pager.title}</b>
					</text>
					<text fg={theme.textMuted}>
						{pager.currentIndex + 1}/{pager.sections.length}
						{noteCount() > 0
							? ` · ${noteCount()} note${noteCount() === 1 ? "" : "s"}`
							: ""}
					</text>
				</box>

				{/* Section dots */}
				<box flexShrink={0} flexDirection="row" gap={1} paddingX={1}>
					<For each={pager.sections}>
						{(_, idx) => {
							const isCurrent = () => idx() === pager.currentIndex;
							const hasNote = () => pager.notes.has(idx());
							const color = () => {
								if (isCurrent())
									return hasNote() ? theme.borderAccent : theme.textPrimary;
								return hasNote() ? theme.toolText : theme.textMuted;
							};
							return (
								<text fg={color()}>
									{isCurrent()
										? hasNote()
											? "◆"
											: "●"
										: hasNote()
											? "●"
											: "○"}
								</text>
							);
						}}
					</For>
				</box>

				{/* Scrollable section content */}
				<scrollbox
					ref={bindScroll}
					flexGrow={1}
					scrollY
					stickyStart="top"
					paddingX={2}
					paddingY={1}
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
						<Show when={pager.currentSection?.sectionTitle}>
							<text fg={theme.textMuted}>
								<b>{pager.currentSection?.sectionTitle}</b>
							</text>
						</Show>
						<code
							filetype="markdown"
							content={pager.currentSection?.body ?? ""}
							syntaxStyle={syntaxStyle}
							conceal
							drawUnstyledText={false}
							fg={theme.textPrimary}
						/>
					</box>
				</scrollbox>

				{/* Note area */}
				<text fg={theme.borderDefault}>{"─".repeat(80)}</text>
				<box flexShrink={0} flexDirection="column" paddingX={1} gap={0}>
					<Show when={mode() === "navigate"}>
						<text fg={theme.textMuted}>
							{currentNote()
								? `Note: ${currentNote()}`
								: "(press n to add a note for this section)"}
						</text>
					</Show>
					<Show when={mode() === "edit"}>
						<text fg={theme.borderAccent}>Note:</text>
						{/* @ts-ignore onPaste supported but not typed */}
						<textarea
							ref={(el) => {
								textareaRef = el as typeof textareaRef;
								try {
									textareaRef?.setText(noteText());
								} catch {
									textareaRef = undefined;
								}
							}}
							minHeight={3}
							maxHeight={6}
							placeholder="Type your note..."
							placeholderColor={theme.textPlaceholder}
							backgroundColor={theme.bg}
							focusedBackgroundColor={theme.bg}
							textColor={theme.textPrimary}
							focusedTextColor={theme.textPrimary}
							cursorColor={theme.cursor}
							showCursor
							wrapMode="word"
							focused={mode() === "edit"}
							keyBindings={[{ name: "return", shift: true, action: "newline" }]}
							onContentChange={() => setNoteText(textareaRef?.plainText ?? "")}
							onPaste={handlePaste}
						/>
					</Show>
				</box>

				{/* Hints */}
				<box flexShrink={0} paddingX={1} paddingBottom={0}>
					<text fg={theme.textMuted}>
						{mode() === "edit"
							? "Shift+Enter newline · Esc back · Ctrl+Enter submit notes"
							: "←/→ section · j/k scroll · n note · Ctrl+Enter submit · Esc close"}
					</text>
				</box>
			</box>
		</Show>
	);
}
