import type { JSX } from "solid-js";
import { theme } from "../theme";

export function ReviewNoteBox(props: {
	children: JSX.Element;
	height?: number;
	marginX?: number;
}) {
	return (
		<box
			border
			borderColor={theme.borderDefault}
			backgroundColor={theme.bgSurface}
			paddingX={1}
			marginX={props.marginX}
			width={props.marginX == null ? "100%" : undefined}
			height={props.height}
			flexShrink={0}
		>
			{props.children}
		</box>
	);
}
