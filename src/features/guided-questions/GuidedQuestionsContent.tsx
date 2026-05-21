import type { PasteEvent } from "@opentui/core";
import { useBindings, useKeymap } from "@opentui/keymap/solid";
import { createEffect, createMemo, createSignal, For, Show } from "solid-js";
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
import { Dialog } from "../../shell/Dialog";
import { CHEVRON_RIGHT } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import type { GuidedQuestionsController } from "./controller";

export type GuidedQuestionsContentProps = {
	guidedQuestions: GuidedQuestionsController;
	settings?: Settings;
	onKeybindingDiagnostic?: (diagnostic: KeybindingDiagnostic) => void;
	onClose: () => void;
	surfaceProps?: OverlaySurfaceProps;
};

export function GuidedQuestionsContent(props: GuidedQuestionsContentProps) {
	const g = props.guidedQuestions;
	const keymap = useKeymap();
	const [textValue, setTextValue] = createSignal("");
	let textareaRef:
		| { plainText: string; setText: (value: string) => void }
		| undefined;

	createEffect(() => {
		if (!g.active) {
			setTextValue("");
			textareaRef = undefined;
			props.onClose();
			return;
		}

		const question = g.currentQuestion;
		if (!question || (g.mode !== "text" && g.mode !== "otherText")) {
			setTextValue("");
			textareaRef = undefined;
			return;
		}

		const existing = g.answers[question.id];
		const nextValue = typeof existing === "string" ? existing : "";
		setTextValue(nextValue);
		try {
			textareaRef?.setText(nextValue);
		} catch {
			textareaRef = undefined;
		}
	});

	const isMultiSelectQuestion = () =>
		g.currentQuestion?.kind === "multiselect" && g.mode !== "otherText";

	function cancel() {
		g.cancel();
		props.onClose();
	}

	const previousCommand = {
		binding: {
			cmd: "guided-questions.previous",
			key: "shift+tab",
			desc: "Go to previous question",
			group: "guided-questions",
		},
		command: {
			hint: "previous",
			run: g.movePrev,
		},
	} as const satisfies CommandBindingDefinition;
	const selectCommands = [
		previousCommand,
		{
			binding: {
				cmd: "guided-questions.cancel",
				key: "escape",
				desc: "Cancel guided questions",
				group: "guided-questions",
			},
			command: {
				hint: "cancel",
				run: cancel,
			},
		},
		{
			binding: {
				cmd: "guided-questions.move-up",
				key: "up",
				desc: "Move to previous option",
				group: "guided-questions",
			},
			command: {
				hint: "move",
				run: g.moveSelectUp,
			},
		},
		{
			binding: {
				cmd: "guided-questions.move-down",
				key: "down",
				desc: "Move to next option",
				group: "guided-questions",
			},
			command: {
				hint: "move",
				run: g.moveSelectDown,
			},
		},
		{
			binding: {
				cmd: "guided-questions.select",
				key: "return",
				desc: "Select focused option",
				group: "guided-questions",
			},
			command: {
				hint: "select",
				run: g.selectOption,
			},
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const multiselectCommands = [
		previousCommand,
		{
			binding: {
				cmd: "guided-questions.cancel",
				key: "escape",
				desc: "Cancel guided questions",
				group: "guided-questions",
			},
			command: {
				hint: "cancel",
				run: cancel,
			},
		},
		{
			binding: {
				cmd: "guided-questions.move-up",
				key: "up",
				desc: "Move to previous option",
				group: "guided-questions",
			},
			command: {
				hint: "move",
				run: g.moveSelectUp,
			},
		},
		{
			binding: {
				cmd: "guided-questions.move-down",
				key: "down",
				desc: "Move to next option",
				group: "guided-questions",
			},
			command: {
				hint: "move",
				run: g.moveSelectDown,
			},
		},
		{
			binding: {
				cmd: "guided-questions.toggle-option",
				key: "space",
				desc: "Toggle focused option",
				group: "guided-questions",
			},
			command: {
				hint: "toggle",
				run: g.selectOption,
			},
		},
		{
			binding: {
				cmd: "guided-questions.confirm-multiselect",
				key: "return",
				desc: "Confirm selected options",
				group: "guided-questions",
			},
			command: {
				hint: "confirm",
				run: g.submitMultiSelect,
			},
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const textCommands = [
		previousCommand,
		{
			binding: {
				cmd: "guided-questions.cancel",
				key: "escape",
				desc: "Cancel guided questions",
				group: "guided-questions",
			},
			command: {
				hint: "cancel",
				run: cancel,
			},
		},
		{
			binding: {
				cmd: "guided-questions.submit-text",
				key: "return",
				desc: "Submit text answer",
				group: "guided-questions",
			},
			command: {
				hint: "submit",
				run: () => g.submitText(textValue()),
			},
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const otherTextCommands = [
		previousCommand,
		{
			binding: {
				cmd: "guided-questions.back",
				key: "escape",
				desc: "Return to option selection",
				group: "guided-questions",
			},
			command: {
				hint: "back",
				run: g.escapeTextMode,
			},
		},
		{
			binding: {
				cmd: "guided-questions.submit-text",
				key: "return",
				desc: "Submit text answer",
				group: "guided-questions",
			},
			command: {
				hint: "submit",
				run: () => g.submitText(textValue()),
			},
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const selectBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			selectCommands,
			props.settings?.keybindings,
		),
	);
	const multiselectBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			multiselectCommands,
			props.settings?.keybindings,
		),
	);
	const textBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			textCommands,
			props.settings?.keybindings,
		),
	);
	const otherTextBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			otherTextCommands,
			props.settings?.keybindings,
		),
	);

	createEffect(() => {
		if (!g.active) return;
		const diagnostics = isMultiSelectQuestion()
			? multiselectBindings().diagnostics
			: g.mode === "text"
				? textBindings().diagnostics
				: g.mode === "otherText"
					? otherTextBindings().diagnostics
					: selectBindings().diagnostics;
		reportKeybindingDiagnostics(diagnostics, props.onKeybindingDiagnostic);
	});
	useBindings(() =>
		withKitKeyAliases({
			enabled: () =>
				g.active && g.mode === "select" && !isMultiSelectQuestion(),
			priority: 200,
			commands: createKeymapCommands(selectCommands),
			bindings: selectBindings().bindings,
		}),
	);
	useBindings(() =>
		withKitKeyAliases({
			enabled: () => g.active && isMultiSelectQuestion(),
			priority: 200,
			commands: createKeymapCommands(multiselectCommands),
			bindings: multiselectBindings().bindings,
		}),
	);
	useBindings(() =>
		withKitKeyAliases({
			enabled: () => g.active && g.mode === "text",
			priority: 200,
			commands: createKeymapCommands(textCommands),
			bindings: textBindings().bindings,
		}),
	);
	useBindings(() =>
		withKitKeyAliases({
			enabled: () => g.active && g.mode === "otherText",
			priority: 200,
			commands: createKeymapCommands(otherTextCommands),
			bindings: otherTextBindings().bindings,
		}),
	);

	function handlePaste(event: PasteEvent) {
		if (g.mode !== "text" && g.mode !== "otherText") return;
		const pasted = new TextDecoder()
			.decode(event.bytes)
			.replace(/\r\n/g, "\n")
			.replace(/\r/g, "\n");
		setTextValue((current) => `${current}${pasted}`);
	}

	const selectOptions = () => {
		const question = g.currentQuestion;
		return question ? g.getSelectOptions(question) : [];
	};

	const focusedIndex = () => {
		const question = g.currentQuestion;
		return question ? g.getValidSelectIndex(question) : -1;
	};

	const placeholder = () => {
		if (g.mode === "otherText") return "Type your custom answer...";
		return g.currentQuestion?.placeholder || "Type your answer...";
	};

	return (
		<Show when={g.active}>
			<Dialog.Root surfaceProps={props.surfaceProps}>
				<Dialog.Header>
					<Dialog.Title>{g.title}</Dialog.Title>
				</Dialog.Header>
				<Show when={g.intro}>
					<text fg={theme.textMuted}>{g.intro}</text>
				</Show>
				<text fg={theme.textMuted}>
					{g.currentIndex + 1}/{g.questions.length} · {g.answeredCount} answered
				</text>

				<Show when={g.currentQuestion}>
					<box flexDirection="column" gap={0}>
						<text fg={theme.textPrimary}>{g.currentQuestion?.label}</text>
						<Show when={g.currentQuestion?.help}>
							<text fg={theme.textMuted}>{g.currentQuestion?.help}</text>
						</Show>
					</box>
				</Show>

				<Show when={g.mode === "select" || isMultiSelectQuestion()}>
					<box flexDirection="column">
						<For each={selectOptions()}>
							{(option, idx) => {
								const isFocused = () => idx() === focusedIndex();
								const isSelected = () =>
									isMultiSelectQuestion() ? g.isOptionSelected(option) : false;
								return (
									<box
										backgroundColor={
											isFocused() ? theme.pickerFocusedBg : theme.bgTransparent
										}
									>
										<text
											fg={
												isFocused()
													? theme.pickerFocusedText
													: theme.textPrimary
											}
											bg={
												isFocused()
													? theme.pickerFocusedBg
													: theme.bgTransparent
											}
										>
											{isFocused() ? `${CHEVRON_RIGHT} ` : "  "}
											{isMultiSelectQuestion()
												? `${isSelected() ? "[x]" : "[ ]"} ${option}`
												: option}
										</text>
									</box>
								);
							}}
						</For>
					</box>
				</Show>

				<Show when={g.mode === "text" || g.mode === "otherText"}>
					<Show when={g.mode === "otherText"}>
						<text fg={theme.borderAccent}>Specify Other:</text>
					</Show>
					<textarea
						ref={(value) => {
							textareaRef = value as typeof textareaRef;
							try {
								textareaRef?.setText(textValue());
							} catch {
								textareaRef = undefined;
							}
						}}
						minHeight={3}
						maxHeight={8}
						placeholder={placeholder()}
						placeholderColor={theme.textPlaceholder}
						backgroundColor={theme.bg}
						focusedBackgroundColor={theme.bg}
						textColor={theme.textPrimary}
						focusedTextColor={theme.textPrimary}
						cursorColor={theme.cursor}
						showCursor
						wrapMode="word"
						focused={g.active}
						keyBindings={[{ name: "return", shift: true, action: "newline" }]}
						onContentChange={() => setTextValue(textareaRef?.plainText ?? "")}
						onPaste={handlePaste}
					/>
				</Show>

				<Dialog.Footer>
					<KeymapHintBar borderless group="guided-questions" />
				</Dialog.Footer>
			</Dialog.Root>
		</Show>
	);
}
