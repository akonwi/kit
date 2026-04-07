import { createSignal } from "solid-js";
import type { FooterStatusState } from "../state/app-state";
import { theme } from "./theme";

const BORDER_CHAR = "─";
const BLUE = "#5599dd";
const ORANGE = "#dd8833";
const RED = "#dd3333";

function progressColor(pct: number): string {
	if (pct > 80) return RED;
	if (pct > 77) return ORANGE;
	return BLUE;
}

export type BottomStatusBarProps = {
	status: FooterStatusState;
};

export function BottomStatusBar(props: BottomStatusBarProps) {
	const [barWidth, setBarWidth] = createSignal(80);
	let boxRef: { width: number } | undefined;

	const pct = () => {
		const n = parseInt(props.status.contextPct, 10);
		return isNaN(n) ? 0 : n;
	};

	const filledWidth = () => Math.round((pct() / 100) * barWidth());
	const emptyWidth = () => barWidth() - filledWidth();

	const bell = () => (props.status.bellsEnabled ? "🔔" : "🔕");
	const speech = () => (props.status.speechEnabled ? "🗣" : "🤫");
	const pending = () =>
		props.status.pendingMessages > 0
			? ` 📬${props.status.pendingMessages}`
			: "";

	// Inner width = box width minus 2 for side borders
	const innerWidth = () => Math.max(0, barWidth() - 2);
	const filled = () => Math.round((pct() / 100) * innerWidth());

	return (
		<box
			flexShrink={0}
			ref={(r) => {
				boxRef = r as typeof boxRef;
			}}
			onSizeChange={() => {
				if (boxRef) setBarWidth(boxRef.width);
			}}
		>
			<box border borderColor={theme.borderStatus} paddingX={1}>
				<text fg={theme.textMuted}>
					{props.status.model} ({props.status.thinkingLevel}){" "}
					{props.status.contextPct}
					{pending()} {bell()} {speech()}
				</text>
			</box>

			{/* Progress overlay on top border */}
			<text position="absolute" top={0} left={1} fg={progressColor(pct())}>
				{BORDER_CHAR.repeat(filled())}
			</text>
		</box>
	);
}
