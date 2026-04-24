import type { JSX } from "solid-js";
import { Show } from "solid-js";
import { theme } from "./theme";

export type ScreenLayoutProps = {
	header?: JSX.Element;
	footer?: JSX.Element;
	children: JSX.Element;
	zIndex?: number;
	backgroundColor?: string;
};

/**
 * Full-screen layout container for Tier 3 screens.
 * Provides a vertical flex column that fills the viewport
 * with fixed header/footer slots and a flexible content area.
 */
export function ScreenLayout(props: ScreenLayoutProps) {
	return (
		<box
			position="absolute"
			top={0}
			left={0}
			width="100%"
			height="100%"
			zIndex={props.zIndex ?? 0}
			backgroundColor={props.backgroundColor ?? theme.bg}
			flexDirection="column"
		>
			<Show when={props.header}>
				{props.header}
			</Show>

			<box flexGrow={1} flexDirection="column" overflow="hidden">
				{props.children}
			</box>

			<Show when={props.footer}>
				<box flexShrink={0}>
					{props.footer}
				</box>
			</Show>
		</box>
	);
}
