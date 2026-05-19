import type { JSX } from "solid-js";
import { createSignal, Show } from "solid-js";
import { HORIZONTAL_LINE } from "./glyphs";
import { theme } from "./theme";

export type ScreenHeaderProps = {
	left: JSX.Element;
	right?: JSX.Element;
	progress?: number;
	progressColor?: string;
	onHeightChange?: (height: number) => void;
};

/**
 * Bordered header bar for Tier 3 screens.
 * Displays left/right content with an optional progress bar
 * rendered as a colored overlay on the top border.
 */
export function ScreenHeader(props: ScreenHeaderProps) {
	const [barWidth, setBarWidth] = createSignal(80);
	let ref: { width: number; height: number } | undefined;

	const innerWidth = () => Math.max(0, barWidth() - 2);
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
				border
				borderColor={theme.borderDefault}
				paddingX={1}
				width="100%"
				flexDirection="row"
				flexWrap="wrap"
				justifyContent="space-between"
				gap={1}
			>
				<box flexGrow={1} flexShrink={0} maxWidth="100%" overflow="hidden">
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
					top={0}
					left={1}
					fg={props.progressColor ?? theme.borderAccent}
				>
					{HORIZONTAL_LINE.repeat(filled())}
				</text>
			</Show>
		</box>
	);
}
