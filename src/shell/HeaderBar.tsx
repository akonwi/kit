import type { FooterStatusState } from "../state/app-state";
import { ScreenHeader } from "./ScreenHeader";
import { theme } from "./theme";

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

	return (
		<ScreenHeader
			left={
				<text fg={theme.textMuted}>
					{props.sessionName || "Unnamed session"}
				</text>
			}
			right={
				<box flexDirection="row" gap={1}>
					<text fg={theme.textMuted}>
						{props.status.model} ({props.status.thinkingLevel})
					</text>
					<text fg={progressColor(pct())}>{props.status.contextPct}</text>
					<text fg={theme.textMuted}>{bell()} {speech()}</text>
				</box>
			}
			progress={pct()}
			progressColor={progressColor(pct())}
			onHeightChange={props.onHeightChange}
		/>
	);
}
