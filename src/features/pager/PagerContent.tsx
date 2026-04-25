import type { KeyEvent, PasteEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { createEffect, createSignal, Show } from "solid-js";
import { type Binding, HintBar } from "../../shell/HintBar";
import { MessageComposer } from "../../shell/MessageComposer";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { syntaxStyle, theme } from "../../shell/theme";
import type { PagerController } from "./pager-controller";

const MODE_BINDINGS: { [key in "navigate" | "edit"]: Binding[] } = {
	navigate: [
		{ key: "←/→", action: "section" },
		{ key: "j/k", action: "scroll" },
		{ key: "n", action: "note" },
		{ key: "Ctrl+Enter", action: "submit" },
		{ key: "Esc", action: "close" },
	],
	edit: [
		{ key: "Shift+Enter", action: "newline" },
		{ key: "Esc", action: "back" },
		{ key: "Ctrl+Enter", action: "submit notes" },
	],
};

export type PagerContentProps = {
	pager: PagerController;
	onClose: () => void;
};

export function PagerContent(props: PagerContentProps) {
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
		props.onClose();
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
			props.onClose();
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
		const pasted = new TextDecoder()
			.decode(event.bytes)
			.replace(/\r\n/g, "\n")
			.replace(/\r/g, "\n");
		setNoteText((cur) => `${cur}${pasted}`);
	}

	const noteCount = () => pager.getNoteCount();
	const currentNote = () => pager.notes.get(pager.currentIndex) ?? "";

	const sectionPct = () => {
		const total = pager.sections.length;
		if (total <= 1) return 100;
		return Math.round(((pager.currentIndex + 1) / total) * 100);
	};

	return (
		<Show when={pager.active}>
			<ScreenLayout
				zIndex={1200}
				header={
					<ScreenHeader
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
					<box flexDirection="column" gap={0}>
						<Show
							when={mode() === "edit"}
							fallback={
								<MessageComposer
									placeholder={currentNote() || "press n to add a note"}
									maxHeight={6}
									focused={false}
									showCursor={false}
								/>
							}
						>
							<MessageComposer
								ref={(el) => {
									textareaRef = el as typeof textareaRef;
								}}
								initialValue={noteText()}
								placeholder="Type your note..."
								maxHeight={6}
								keyBindings={[
									{ name: "return", shift: true, action: "newline" },
								]}
								onContentChange={() =>
									setNoteText(textareaRef?.plainText ?? "")
								}
								onPaste={handlePaste}
							/>
						</Show>
						<HintBar bindings={MODE_BINDINGS[mode()]} />
					</box>
				}
			>
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
						<markdown
							content={pager.currentSection?.body ?? ""}
							syntaxStyle={syntaxStyle()}
							conceal
							fg={theme.textPrimary}
						/>
					</box>
				</scrollbox>
			</ScreenLayout>
		</Show>
	);
}
