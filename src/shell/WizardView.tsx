import { For, Show } from "solid-js";
import type { WizardController } from "../features/wizard";
import { theme } from "./theme";

export type WizardViewProps = {
	wizard: WizardController;
};

export function WizardView(props: WizardViewProps) {
	const w = props.wizard;

	return (
		<box flexGrow={1} height="100%" flexDirection="column">
			<box flexGrow={1} flexDirection="column" padding={1} gap={1}>
				{/* Title */}
				<text fg={theme.textPrimary}>
					<b>{w.title}</b>
				</text>

				{/* Intro */}
				<Show when={w.intro}>
					<text fg={theme.textMuted}>{w.intro}</text>
				</Show>

				{/* Progress dots */}
				<box flexDirection="row" gap={1}>
					<For each={w.questions}>
						{(_, idx) => {
							const isCurrent = () => idx() === w.currentIndex;
							const isAnswered = () => {
								const q = w.questions[idx()];
								if (!q) return false;
								const v = w.answers[q.id];
								if (typeof v === "boolean") return true;
								return typeof v === "string" && v.trim().length > 0;
							};
							const color = () => {
								if (isCurrent()) return theme.textPrimary;
								return isAnswered() ? theme.toolText : theme.textMuted;
							};
							return (
								<text fg={color()}>
									{isCurrent() ? "●" : isAnswered() ? "●" : "○"}
								</text>
							);
						}}
					</For>
					<text fg={theme.textMuted}>
						{w.currentIndex + 1}/{w.questions.length} · {w.answeredCount}{" "}
						answered
					</text>
				</box>

				{/* Current question */}
				<Show when={w.currentQuestion}>
					<box flexDirection="column" gap={0} paddingTop={1}>
						<text fg={theme.textPrimary}>
							<b>{w.currentQuestion!.label}</b>
						</text>
						<Show when={w.currentQuestion!.help}>
							<text fg={theme.textMuted}>{w.currentQuestion!.help}</text>
						</Show>
					</box>
				</Show>
			</box>

			{/* Fixed footer hints */}
			<box flexShrink={0} paddingX={1} paddingBottom={0}>
				<text fg={theme.textMuted}>
					{w.mode === "select"
						? "↑/↓ move · Enter select · Shift+Tab prev · Escape cancel"
						: w.mode === "otherText"
							? "Enter submit · Escape back to options · Shift+Tab prev"
							: "Enter submit · Shift+Tab prev · Escape cancel"}
				</text>
			</box>
		</box>
	);
}
