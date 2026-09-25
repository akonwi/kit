import type { PasteEvent } from "@opentui/core";
import { createEffect, createMemo, createSignal, For, Show } from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { InternalPluginInteractionComponentProps } from "../../plugins/types";
import { CHEVRON_RIGHT } from "../../shell/glyphs";
import { InteractionDock } from "../../shell/InteractionDock";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import type { GuidedQuestionsController } from "./controller";

const GUIDED_MAX_VISIBLE_OPTIONS = 8;

export type GuidedQuestionsContentProps = {
	guidedQuestions: GuidedQuestionsController;
	onClose: () => void;
	active: InternalPluginInteractionComponentProps<unknown>["active"];
	maxHeight: InternalPluginInteractionComponentProps<unknown>["maxHeight"];
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
		props.active && g.active && g.mode === "select" && !isMultiSelectQuestion();
	const multiselectModeActive = () =>
		props.active && g.active && isMultiSelectQuestion();
	const textModeActive = () => props.active && g.active && g.mode === "text";
	const otherTextModeActive = () =>
		props.active && g.active && g.mode === "otherText";

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

	const visibleSelectOptions = createMemo(() => {
		const options = selectOptions();
		const visibleCount = Math.max(
			1,
			Math.min(
				GUIDED_MAX_VISIBLE_OPTIONS,
				props.maxHeight -
					7 -
					(g.intro ? 1 : 0) -
					(g.currentQuestion?.help ? 1 : 0),
			),
		);
		if (options.length <= visibleCount) {
			return options.map((option, index) => ({ option, index }));
		}
		const focused = focusedIndex();
		let offset = focused - Math.floor(visibleCount / 2);
		offset = Math.max(0, Math.min(offset, options.length - visibleCount));
		return options
			.slice(offset, offset + visibleCount)
			.map((option, index) => ({ option, index: offset + index }));
	});

	const placeholder = () => {
		if (g.mode === "otherText") return "Type your custom answer...";
		return g.currentQuestion?.placeholder || "Type your answer...";
	};
	const textInputHeight = () =>
		Math.max(
			1,
			Math.min(
				8,
				props.maxHeight -
					7 -
					(g.intro ? 1 : 0) -
					(g.currentQuestion?.help ? 1 : 0) -
					(g.mode === "otherText" ? 1 : 0),
			),
		);

	return (
		<Show when={g.active}>
			<InteractionDock.Root maxHeight={props.maxHeight}>
				<InteractionDock.Header
					meta={
						<InteractionDock.Meta>
							{g.currentIndex + 1}/{g.questions.length} · {g.answeredCount}{" "}
							answered
						</InteractionDock.Meta>
					}
				>
					<InteractionDock.Title>{g.title}</InteractionDock.Title>
				</InteractionDock.Header>
				<InteractionDock.Body>
					<Show when={g.intro}>
						<text fg={theme.textMuted} wrapMode="none">
							{g.intro}
						</text>
					</Show>

					<Show when={g.currentQuestion}>
						<box flexDirection="column" gap={0}>
							<text fg={theme.textPrimary} wrapMode="none">
								{g.currentQuestion?.label}
							</text>
							<Show when={g.currentQuestion?.help}>
								<text fg={theme.textMuted} wrapMode="none">
									{g.currentQuestion?.help}
								</text>
							</Show>
						</box>
					</Show>

					<Show when={g.mode === "select" || isMultiSelectQuestion()}>
						<box flexDirection="column">
							<For each={visibleSelectOptions()}>
								{(entry) => {
									const isFocused = () => entry.index === focusedIndex();
									const isSelected = () =>
										isMultiSelectQuestion()
											? g.isOptionSelected(entry.option)
											: false;
									return (
										<box
											backgroundColor={
												isFocused()
													? theme.pickerFocusedBg
													: theme.bgTransparent
											}
										>
											<text
												wrapMode="none"
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
													? `${isSelected() ? "[x]" : "[ ]"} ${entry.option}`
													: entry.option}
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
							minHeight={1}
							maxHeight={textInputHeight()}
							placeholder={placeholder()}
							placeholderColor={theme.textPlaceholder}
							backgroundColor={theme.bg}
							focusedBackgroundColor={theme.bg}
							textColor={theme.textPrimary}
							focusedTextColor={theme.textPrimary}
							cursorColor={theme.cursor}
							showCursor
							wrapMode="word"
							focused={props.active && g.active}
							keyBindings={[{ name: "return", shift: true, action: "newline" }]}
							onContentChange={() => setTextValue(textareaRef?.plainText ?? "")}
							onPaste={handlePaste}
						/>
					</Show>
				</InteractionDock.Body>

				<InteractionDock.Footer>
					<KeymapHintBar borderless group="guided-questions" />
				</InteractionDock.Footer>
			</InteractionDock.Root>
		</Show>
	);
}
