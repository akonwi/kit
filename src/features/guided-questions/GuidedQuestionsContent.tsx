import type { KeyEvent, PasteEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { createEffect, createSignal, For, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { DialogFrame } from "../../shell/DialogFrame";
import { type Binding, HintBar } from "../../shell/HintBar";
import { theme } from "../../shell/theme";
import type {
	GuidedQuestionsController,
	GuidedQuestionsMode,
} from "./controller";

const QUESTION_BINDINGS: Record<GuidedQuestionsMode, Binding[]> = {
	multiselect: [
		{ key: "↑/↓", action: "move" },
		{ key: "Space", action: "toggle" },
		{ key: "Enter", action: "confirm" },
		{ key: "Shift+Tab", action: "previous" },
		{ key: "Esc", action: "cancel" },
	],
	select: [
		{ key: "↑/↓", action: "move" },
		{ key: "Enter", action: "select" },
		{ key: "Shift+Tab", action: "previous" },
		{ key: "Esc", action: "cancel" },
	],
	otherText: [
		{ key: "Enter", action: "submit" },
		{ key: "Shift+Enter", action: "newline" },
		{ key: "Esc", action: "back" },
		{ key: "Shift+Tab", action: "previous" },
	],
	text: [
		{ key: "Enter", action: "submit" },
		{ key: "Shift+Enter", action: "newline" },
		{ key: "Shift+Tab", action: "previous" },
		{ key: "Esc", action: "cancel" },
	],
};

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

	useKeyboard((e: KeyEvent) => {
		if (!g.active) return;

		if (e.name === "escape") {
			e.preventDefault();
			if (g.mode === "otherText") g.escapeTextMode();
			else {
				g.cancel();
				props.onClose();
			}
			return;
		}

		if (e.shift && e.name === "tab") {
			e.preventDefault();
			g.movePrev();
			return;
		}

		if (
			g.mode === "select" ||
			g.mode === "multiselect" ||
			isMultiSelectQuestion()
		) {
			if (e.name === "up") {
				e.preventDefault();
				g.moveSelectUp();
				return;
			}
			if (e.name === "down") {
				e.preventDefault();
				g.moveSelectDown();
				return;
			}
			if (isMultiSelectQuestion() && e.name === "space") {
				e.preventDefault();
				g.selectOption();
				return;
			}
			if (
				isMultiSelectQuestion() &&
				(e.name === "return" || e.name === "enter")
			) {
				e.preventDefault();
				g.submitMultiSelect();
				return;
			}
			if (
				!isMultiSelectQuestion() &&
				g.mode === "select" &&
				e.name === "return"
			) {
				e.preventDefault();
				g.selectOption();
				return;
			}
			return;
		}

		if (
			(g.mode === "text" || g.mode === "otherText") &&
			e.name === "return" &&
			!e.shift
		) {
			e.preventDefault();
			g.submitText(textValue());
		}
	});

	function handlePaste(event: PasteEvent) {
		if (g.mode !== "text" && g.mode !== "otherText") return;
		const pasted = new TextDecoder()
			.decode(event.bytes)
			.replace(/\r\n/g, "\n")
			.replace(/\r/g, "\n");
		setTextValue((current) => `${current}${pasted}`);
	}

	const isMultiSelectQuestion = () =>
		g.currentQuestion?.kind === "multiselect" && g.mode !== "otherText";

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
			<DialogFrame surfaceProps={props.surfaceProps}>
				<text fg={theme.textPrimary}>{g.title}</text>
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
											{isFocused() ? "› " : "  "}
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

				<HintBar
					bindings={
						QUESTION_BINDINGS[
							isMultiSelectQuestion() ? "multiselect" : g.mode
						] ?? QUESTION_BINDINGS.text
					}
				/>
			</DialogFrame>
		</Show>
	);
}
