import type { MouseEvent as TuiMouseEvent } from "@opentui/core";
import { createSignal, type JSX } from "solid-js";
import { CHEVRON_LEFT, CHEVRON_RIGHT } from "./glyphs";
import { theme } from "./theme";

export type WorkspacePanelHeaderProps = {
	leading?: JSX.Element;
	left?: JSX.Element;
	right?: JSX.Element;
};

/** A contextual metadata row below workspace tabs. Pane names belong to the tabs. */
export function WorkspacePanelHeader(props: WorkspacePanelHeaderProps) {
	return (
		<box
			flexShrink={0}
			width="100%"
			flexDirection="row"
			border={["bottom"]}
			borderColor={theme.borderDefault}
		>
			{props.leading}
			<box
				flexGrow={1}
				paddingX={1}
				flexDirection="row"
				justifyContent="space-between"
				gap={1}
			>
				<box flexGrow={1} flexShrink={1} height={1} overflow="hidden">
					{props.left}
				</box>
				<box flexShrink={1} height={1} overflow="hidden">
					{props.right}
				</box>
			</box>
		</box>
	);
}

export function WorkspaceSidebarToggle(props: {
	expanded: boolean;
	onToggle: () => void;
}) {
	const [hovered, setHovered] = createSignal(false);

	function toggle(event: TuiMouseEvent): void {
		if (event.button !== 0) return;
		event.preventDefault();
		props.onToggle();
	}
	return (
		<box
			width={3}
			flexShrink={0}
			alignItems="center"
			justifyContent="center"
			backgroundColor={hovered() ? theme.bgMuted : theme.bgSurface}
			onMouseOver={() => setHovered(true)}
			onMouseOut={() => setHovered(false)}
			onMouseDown={toggle}
		>
			<text fg={hovered() ? theme.textPrimary : theme.textSecondary}>
				{props.expanded ? CHEVRON_LEFT : CHEVRON_RIGHT}
			</text>
		</box>
	);
}

export type WorkspacePanelLayoutProps = {
	header?: JSX.Element;
	footer: JSX.Element;
	children: JSX.Element;
};

/** Shared optional-context/body/footer structure for persistent workspace panels. */
export function WorkspacePanelLayout(props: WorkspacePanelLayoutProps) {
	return (
		<box
			width="100%"
			height="100%"
			backgroundColor={theme.bg}
			flexDirection="column"
		>
			{props.header}
			<box flexGrow={1} flexDirection="column" overflow="hidden">
				{props.children}
			</box>
			<box
				flexShrink={0}
				paddingX={1}
				border={["top"]}
				borderColor={theme.borderDefault}
			>
				{props.footer}
			</box>
		</box>
	);
}
