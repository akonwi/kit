import type { JSX } from "solid-js";
import { theme } from "./theme";

export type WorkspacePanelHeaderProps = {
	left?: JSX.Element;
	right?: JSX.Element;
};

/** A contextual metadata row below workspace tabs. Pane names belong to the tabs. */
export function WorkspacePanelHeader(props: WorkspacePanelHeaderProps) {
	return (
		<box
			flexShrink={0}
			paddingX={1}
			width="100%"
			flexDirection="row"
			border={["bottom"]}
			borderColor={theme.borderDefault}
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
