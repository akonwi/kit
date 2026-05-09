import type { BoxProps } from "@opentui/solid";
import { theme } from "./theme";

export type Binding = { key: string; action: string };

export type HintBarProps = {
	bindings: Binding[];
	borderless?: boolean;
};

/**
 * Standardized hint bar for keyboard shortcuts.
 * Bordered box with muted text, used at the bottom of screens and dialogs.
 * Renders bindings as "key action · key action · ..."
 */
export function HintBar(props: HintBarProps) {
	const text = () =>
		props.bindings.map((b) => `${b.key} ${b.action}`).join(" · ");
	const styles: BoxProps = props.borderless
		? {
				paddingY: 1,
			}
		: {
				border: true,
				borderColor: theme.borderDefault,
				paddingX: 1,
			};

	return (
		<box {...styles} flexShrink={0}>
			<text fg={theme.textMuted}>{text()}</text>
		</box>
	);
}
