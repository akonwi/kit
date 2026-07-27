import type { JSX } from "solid-js";
import { theme } from "./theme";

export type WorkspacePanelLayoutProps = {
	header: JSX.Element;
	footer: JSX.Element;
	children: JSX.Element;
};

/** Shared header/body/footer structure for persistent workspace panels. */
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
