import type { JSX } from "solid-js";
import { createSignal, Show } from "solid-js";
import { HORIZONTAL_LINE } from "./glyphs";
import { theme } from "./theme";

export type ScreenHeaderProps = {
	left: JSX.Element;
	variant?: "framed" | "strip";
	right?: JSX.Element;
	progress?: number;
	progressColor?: string;
	onHeightChange?: (height: number) => void;
};

/**
 * Header bar for Tier 3 screens. Framed by default; the strip variant uses
 * only a bottom separator. Progress overlays the relevant structural border.
 */
export function ScreenHeader(props: ScreenHeaderProps) {
	const [barWidth, setBarWidth] = createSignal(80);
	const strip = () => props.variant === "strip";
	let ref: { width: number; height: number } | undefined;

	const innerWidth = () => Math.max(0, barWidth() - (strip() ? 0 : 2));
	const clampedProgress = () => {
		const pct = props.progress ?? 0;
		return Math.max(0, Math.min(100, pct));
	};
	const filled = () => {
		return Math.min(
			innerWidth(),
			Math.round((clampedProgress() / 100) * innerWidth()),
		);
	};

	return (
		<box
			flexShrink={0}
			ref={(r) => {
				ref = r as typeof ref;
			}}
			onSizeChange={() => {
				if (ref) {
					setBarWidth(ref.width);
					props.onHeightChange?.(ref.height);
				}
			}}
		>
			<box
				border={strip() ? ["bottom"] : true}
				borderColor={theme.borderDefault}
				paddingX={1}
				width="100%"
				height={strip() ? 2 : undefined}
				flexDirection="row"
				flexWrap={strip() ? "no-wrap" : "wrap"}
				justifyContent="space-between"
				gap={1}
			>
				<box
					flexGrow={1}
					flexShrink={strip() ? 1 : 0}
					maxWidth="100%"
					overflow="hidden"
				>
					{props.left}
				</box>
				<Show when={props.right}>
					<box
						flexShrink={0}
						maxWidth="100%"
						overflow="hidden"
						justifyContent="flex-end"
					>
						{props.right}
					</box>
				</Show>
			</box>

			<Show when={props.progress != null && filled() > 0}>
				<text
					position="absolute"
					top={strip() ? 1 : 0}
					left={strip() ? 0 : 1}
					fg={props.progressColor ?? theme.borderAccent}
				>
					{HORIZONTAL_LINE.repeat(filled())}
				</text>
			</Show>
		</box>
	);
}
