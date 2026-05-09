import type { JSX } from "solid-js";
import type { OverlaySurfaceProps } from "../app/overlay-ui";
import { theme } from "./theme";

export type DialogFrameProps = {
	children: JSX.Element;
	width?: number | `${number}%`;
	maxWidth?: number;
	minWidth?: number;
	height?: number | `${number}%`;
	padding?: number;
	surfaceProps?: OverlaySurfaceProps;
};

/**
 * Tier 2 dialog frame — backdrop + centered surface panel.
 *
 * Provides the standard dialog chrome: full-screen semi-transparent backdrop,
 * percentage-based sizing with min/max constraints, and a `bgSurface` panel
 * with uniform padding. Content is rendered as children inside the panel.
 *
 * Usage:
 * ```tsx
 * <DialogFrame width="70%" maxWidth={96} minWidth={48}>
 *   <text>Title</text>
 *   {content}
 *   <HintBar bindings={bindings} />
 * </DialogFrame>
 * ```
 */
export function DialogFrame(props: DialogFrameProps) {
	return (
		<box
			{...props.surfaceProps}
			position="absolute"
			left={0}
			top={0}
			width="100%"
			height="100%"
			justifyContent="center"
			alignItems="center"
			backgroundColor={theme.modalBackdrop}
		>
			<box
				width={props.width ?? "70%"}
				maxWidth={props.maxWidth ?? 96}
				minWidth={props.minWidth ?? 48}
				height={props.height}
				flexDirection="column"
			>
				<box
					backgroundColor={theme.bgSurface}
					padding={props.padding ?? 1}
					flexDirection="column"
					gap={1}
					flexGrow={1}
				>
					{props.children}
				</box>
			</box>
		</box>
	);
}
