import type { ParentProps } from "solid-js";
import { theme } from "../theme";

/**
 * Card wrapper used inside the turn activity dialog to give each
 * section (tool call, bash, handoff) its own elevated surface.
 *
 * Visually a slightly lighter band against the dialog's bgSurface.
 */
export function DialogCard(props: ParentProps) {
	return (
		<box backgroundColor={theme.bgMuted} paddingX={1} width="100%">
			{props.children}
		</box>
	);
}
