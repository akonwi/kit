import type { PasteEvent } from "@opentui/core";
import { createEffect, createSignal, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { Binding } from "../../shell/HintBar";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { MessageComposer } from "../../shell/MessageComposer";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { scrollbarStyle, syntaxStyle, theme } from "../../shell/theme";
import type { PagerController } from "./pager-controller";

const EDIT_PREFIX_BINDINGS: Binding[] = [
	{ key: "Enter", action: "save" },
	{ key: "Shift+Enter", action: "newline" },
];

export type PagerContentProps = {
	pager: PagerController;
	onClose: () => void;
	surfaceProps?: OverlaySurfaceProps;
};

export function PagerContent(props: PagerContentProps) {
	const pager = props.pager;
	const [mode, setMode] = createSignal<"navigate" | "edit">("navigate");
	const [noteText, setNoteText] = createSignal("");
	let scrollRef:
		| { scrollBy: (opts: { x: number; y: number }) => void }
		| undefined;
	let textareaRef:
		| { plainText: string; setText: (value: string) => void }
		| undefined;

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

	function closePager() {
		pager.closeView();
		props.onClose();
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => pager.active && mode() === "navigate",
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
			pager.active && mode() === "navigate" && currentNote().length > 0,
		commands: {
			"pager.clear-note": clearCurrentNote,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => pager.active && mode() === "navigate" && noteCount() > 0,
		commands: {
			"pager.submit-feedback": closePager,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => pager.active && mode() === "edit",
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
								<box
									flexDirection="column"
									paddingX={1}
									maxHeight={5}
									overflow="hidden"
								>
									<Show
										when={currentNote()}
										fallback={
											<text fg={theme.textMuted}>No note for this section</text>
										}
									>
										<text fg={theme.reviewText}>
											<b>Note</b>
										</text>
										<text fg={theme.textSecondary}>{currentNote()}</text>
									</Show>
								</box>
							}
						>
							<MessageComposer
								ref={(element) => {
									textareaRef = element as typeof textareaRef;
								}}
								initialValue={noteText()}
								placeholder="Type your note..."
								maxHeight={6}
								borderColor={theme.borderAccent}
								keyBindings={[
									{ name: "return", action: "submit" },
									{ name: "return", shift: true, action: "newline" },
								]}
								onContentChange={() =>
									setNoteText(textareaRef?.plainText ?? "")
								}
								onPaste={handlePaste}
								onSubmit={saveNote}
							/>
						</Show>
						<KeymapHintBar
							group="pager"
							prefixBindings={mode() === "edit" ? EDIT_PREFIX_BINDINGS : []}
						/>
					</box>
				}
			>
				<scrollbox
					ref={bindScroll}
					flexGrow={1}
					scrollY
					stickyStart="top"
					paddingX={1}
					paddingY={1}
					style={scrollbarStyle()}
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
