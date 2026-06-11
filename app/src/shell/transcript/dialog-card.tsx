import type { ParentProps } from "solid-js";
import { theme } from "../theme";

/**
 * Card wrapper used inside the turn activity view to give each section
 * (tool call, bash, handoff) its own elevated surface.
 *
 * Visually a slightly lighter band against the panel/dialog background.
 * Carries no padding of its own — horizontal breathing comes from the
 * surrounding view body (modal: Dialog.Root padding; sidebar: body
 * paddingX). Avoids double-padding between panel edge and tool content.
 * Content inside abuts the card edge so the colored band reads as a
 * tight grouping rather than a nested frame.
 */
export function DialogCard(props: ParentProps) {
	return (
		<box backgroundColor={theme.bgMuted} width="100%">
			{props.children}
		</box>
	);
}
