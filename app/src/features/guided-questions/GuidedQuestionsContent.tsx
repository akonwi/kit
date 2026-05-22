import type { PasteEvent } from "@opentui/core";
import { createEffect, createSignal, For, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { Dialog } from "../../shell/Dialog";
import { CHEVRON_RIGHT } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import type { GuidedQuestionsController } from "./controller";

export type GuidedQuestionsContentProps = {
	guidedQuestions: GuidedQuestionsController;
	onClose: () => void;
	surfaceProps?: OverlaySurfaceProps;
};

export function GuidedQuestionsContent(props: GuidedQuestionsContentProps) {
	const g = props.guidedQuestions;
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
	const selectModeActive = () =>
		g.active && g.mode === "select" && !isMultiSelectQuestion();
	const multiselectModeActive = () => g.active && isMultiSelectQuestion();
	const textModeActive = () => g.active && g.mode === "text";
	const otherTextModeActive = () => g.active && g.mode === "otherText";

	function cancel() {
		g.cancel();
		props.onClose();
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: selectModeActive,
		diagnosticsWhen: selectModeActive,
		commands: {
			"guided-questions.previous": g.movePrev,
			"guided-questions.cancel": cancel,
			"guided-questions.move-up": g.moveSelectUp,
			"guided-questions.move-down": g.moveSelectDown,
			"guided-questions.select": g.selectOption,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: multiselectModeActive,
		diagnosticsWhen: multiselectModeActive,
		commands: {
			"guided-questions.previous": g.movePrev,
			"guided-questions.cancel": cancel,
			"guided-questions.move-up": g.moveSelectUp,
			"guided-questions.move-down": g.moveSelectDown,
			"guided-questions.toggle-option": g.selectOption,
			"guided-questions.confirm-multiselect": g.submitMultiSelect,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: textModeActive,
		diagnosticsWhen: textModeActive,
		commands: {
			"guided-questions.previous": g.movePrev,
			"guided-questions.cancel": cancel,
			"guided-questions.submit-text": () => g.submitText(textValue()),
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: otherTextModeActive,
		diagnosticsWhen: otherTextModeActive,
		commands: {
			"guided-questions.previous": g.movePrev,
			"guided-questions.back": g.escapeTextMode,
			"guided-questions.submit-text": () => g.submitText(textValue()),
		},
	}));

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
