import { theme } from "./theme";

export type Binding = { key: string; action: string };

export type HintBarProps = {
	bindings: Binding[];
};

/**
 * Standardized hint bar for keyboard shortcuts.
 * Bordered box with muted text, used at the bottom of screens and dialogs.
 * Renders bindings as "key action · key action · ..."
 */
export function HintBar(props: HintBarProps) {
	const text = () =>
		props.bindings.map((b) => `${b.key} ${b.action}`).join(" · ");

	return (
		<box flexShrink={0} border borderColor={theme.borderDefault} paddingX={1}>
			<text fg={theme.textMuted}>{text()}</text>
		</box>
	);
}
