import type { PasteEvent } from "@opentui/core";
import { useBindings, useKeymap } from "@opentui/keymap/solid";
import { createEffect, createMemo, createSignal, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import {
	type CommandBindingDefinition,
	createConfiguredCommandBindingResult,
	createKeymapCommands,
	type KeybindingDiagnostic,
	withKitKeyAliases,
} from "../../keymap/bindings";
import { reportKeybindingDiagnostics } from "../../keymap/diagnostics";
import type { Settings } from "../../settings";
import type { Binding } from "../../shell/HintBar";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { MessageComposer } from "../../shell/MessageComposer";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { syntaxStyle, theme } from "../../shell/theme";
import type { PagerController } from "./pager-controller";

const EDIT_PREFIX_BINDINGS: Binding[] = [
	{ key: "Shift+Enter", action: "newline" },
];

export type PagerContentProps = {
	pager: PagerController;
	settings?: Settings;
	onKeybindingDiagnostic?: (diagnostic: KeybindingDiagnostic) => void;
	onClose: () => void;
	surfaceProps?: OverlaySurfaceProps;
};

export function PagerContent(props: PagerContentProps) {
	const pager = props.pager;
	const keymap = useKeymap();

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

	function closePager() {
		pager.close();
		props.onClose();
	}

	const navigateCommands = [
		{
			binding: {
				cmd: "pager.previous-section",
				key: ["left", "h"],
				desc: "Show previous pager section",
				group: "pager",
			},
			command: {
				hint: "section",
				run: pager.prevSection,
			},
		},
		{
			binding: {
				cmd: "pager.next-section",
				key: ["right", "l"],
				desc: "Show next pager section",
				group: "pager",
			},
			command: {
				hint: "section",
				run: pager.nextSection,
			},
		},
		{
			binding: {
				cmd: "pager.scroll-up",
				key: ["up", "k"],
				desc: "Scroll pager up",
				group: "pager",
			},
			command: {
				hint: "scroll",
				run: pager.scrollUp,
			},
		},
		{
			binding: {
				cmd: "pager.scroll-down",
				key: ["down", "j"],
				desc: "Scroll pager down",
				group: "pager",
			},
			command: {
				hint: "scroll",
				run: pager.scrollDown,
			},
		},
		{
			binding: {
				cmd: "pager.edit-note",
				key: ["n", "i"],
				desc: "Edit note for current pager section",
				group: "pager",
			},
			command: {
				hint: "note",
				run: enterEditMode,
			},
		},
		{
			binding: {
				cmd: "pager.submit-feedback",
				key: "ctrl+return",
				desc: "Submit pager feedback",
				group: "pager",
			},
			command: {
				hint: "submit",
				run: handleSubmit,
			},
		},
		{
			binding: {
				cmd: "pager.close",
				key: ["escape", "q"],
				desc: "Close pager",
				group: "pager",
			},
			command: {
				hint: "close",
				run: closePager,
			},
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const editCommands = [
		{
			binding: {
				cmd: "pager.back",
				key: "escape",
				desc: "Return to pager navigation",
				group: "pager",
			},
			command: {
				hint: "back",
				run: exitEditMode,
			},
		},
		{
			binding: {
				cmd: "pager.submit-feedback",
				key: "ctrl+return",
				desc: "Submit pager feedback",
				group: "pager",
			},
			command: {
				hint: "submit notes",
				run: handleSubmit,
			},
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const navigateBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			navigateCommands,
			props.settings?.keybindings,
		),
	);
	const editBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			editCommands,
			props.settings?.keybindings,
		),
	);

	createEffect(() => {
		if (!pager.active) return;
		reportKeybindingDiagnostics(
			mode() === "edit"
				? editBindings().diagnostics
				: navigateBindings().diagnostics,
			props.onKeybindingDiagnostic,
		);
	});

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => pager.active && mode() === "navigate",
			priority: 200,
			commands: createKeymapCommands(navigateCommands),
			bindings: navigateBindings().bindings,
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => pager.active && mode() === "edit",
			priority: 200,
			commands: createKeymapCommands(editCommands),
			bindings: editBindings().bindings,
		}),
	);

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
