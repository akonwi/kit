import { For, Show } from "solid-js";
import type {
	ChromeContribution,
	ChromeTextStyle,
} from "./chrome-contributions";
import { MIDDLE_DOT } from "./glyphs";
import { theme } from "./theme";

type ChromeContributionLineProps = {
	contributions: readonly ChromeContribution[];
	fg?: string;
	separatorFg?: string;
	fallback?: string;
	wrap?: boolean;
};

function handleClick(contribution: ChromeContribution) {
	void contribution.onClick?.();
}

function focusedSegmentStyle(
	style: ChromeTextStyle | undefined,
): ChromeTextStyle {
	if (!style) return {};
	const { fg: _fg, bg: _bg, ...attributes } = style;
	return attributes;
}

export function ChromeContributionText(props: {
	contribution: ChromeContribution;
	fg?: string;
	focused?: boolean;
}) {
	return (
		<text
			fg={props.focused ? theme.pickerFocusedText : props.fg}
			bg={props.focused ? theme.pickerFocusedBg : undefined}
			onMouseDown={
				props.contribution.onClick
					? (event) => {
							event.preventDefault();
							event.stopPropagation();
						}
					: undefined
			}
			onMouseUp={
				props.contribution.onClick
					? () => handleClick(props.contribution)
					: undefined
			}
		>
			<For each={props.contribution.content}>
				{(segment) => (
					// The Solid reconciler only applies span styling through the
					// `style` object prop; direct fg/bg/attributes props are
					// silently ignored on text nodes.
					<span
						style={
							props.focused
								? focusedSegmentStyle(segment.style)
								: (segment.style ?? {})
						}
					>
						{segment.text}
					</span>
				)}
			</For>
		</text>
	);
}

export function ChromeContributionLine(props: ChromeContributionLineProps) {
	return (
		<Show
			when={props.contributions.length > 0}
			fallback={<text fg={props.fg}>{props.fallback ?? " "}</text>}
		>
			<box
				flexDirection="row"
				flexWrap={props.wrap === false ? "no-wrap" : "wrap"}
				maxWidth="100%"
				overflow="hidden"
			>
				<For each={props.contributions}>
					{(contribution, index) => (
						<>
							{index() > 0 ? (
								<text fg={props.separatorFg ?? theme.textMuted}>
									{` ${MIDDLE_DOT} `}
								</text>
							) : null}
							<ChromeContributionText
								contribution={contribution}
								fg={props.fg}
							/>
						</>
					)}
				</For>
			</box>
		</Show>
	);
}
