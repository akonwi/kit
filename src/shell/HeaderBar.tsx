import { Show } from "solid-js";
import { codeReviewStatus } from "../features/code-review/state";
import type { FooterStatusState } from "../state/app-state";
import { ScreenHeader } from "./ScreenHeader";
import { theme } from "./theme";

// TODO: Replace this direct global feature-state import once plugins can expose
// header/footer contributions or plugin state can be queried through
// PluginManager in the component tree.

function progressColor(pct: number): string {
	if (pct > 90) return theme.progressCritical;
	if (pct >= 80) return theme.progressWarning;
	return theme.progressNormal;
}

export type HeaderBarProps = {
	sessionName: string | undefined;
	status: FooterStatusState;
	onHeightChange?: (height: number) => void;
};

export function HeaderBar(props: HeaderBarProps) {
	const pct = () => {
		const n = parseInt(props.status.contextPct, 10);
		return Number.isNaN(n) ? 0 : n;
	};

	const bell = () => (props.status.bellsEnabled ? "🔔" : "🔕");
	const speech = () => (props.status.speechEnabled ? "🗣" : "🤫");
	const reviewVisible = () =>
		codeReviewStatus.launchInFlight ||
		codeReviewStatus.serverState === "ready" ||
		codeReviewStatus.serverState === "error";
	const reviewLabel = () => {
		if (codeReviewStatus.serverState === "error") return "✗ review";
		if (codeReviewStatus.launchInFlight) return "◌ review…";
		if (codeReviewStatus.clientConnected) {
			return `◉ review${codeReviewStatus.port ? ` :${codeReviewStatus.port}` : ""}`;
		}
		return `◌ review${codeReviewStatus.port ? ` :${codeReviewStatus.port}` : ""}`;
	};
	const reviewColor = () => {
		if (codeReviewStatus.serverState === "error") return theme.errorText;
		if (codeReviewStatus.launchInFlight) return theme.warningText;
		if (codeReviewStatus.clientConnected) return theme.toolText;
		return theme.textMuted;
	};

	return (
		<ScreenHeader
			left={
				<text fg={theme.textMuted}>
					{props.sessionName || "Unnamed session"}
				</text>
			}
			right={
				<box flexDirection="row" gap={1}>
					<Show when={reviewVisible()}>
						<text fg={reviewColor()}>{reviewLabel()}</text>
					</Show>
					<text fg={theme.textMuted}>
						{props.status.model} ({props.status.thinkingLevel})
					</text>
					<text fg={progressColor(pct())}>{props.status.contextPct}</text>
					<text fg={theme.textMuted}>
						{bell()} {speech()}
					</text>
				</box>
			}
			progress={pct()}
			progressColor={progressColor(pct())}
			onHeightChange={props.onHeightChange}
		/>
	);
}
