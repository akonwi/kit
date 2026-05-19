import { createTextAttributes } from "@opentui/core";
import { For, Show } from "solid-js";
import type {
	ChromeContribution,
	ChromeTextSegment,
} from "./chrome-contributions";
import { MIDDLE_DOT } from "./glyphs";
import { theme } from "./theme";

type ChromeContributionLineProps = {
	contributions: readonly ChromeContribution[];
	fg?: string;
	separatorFg?: string;
	fallback?: string;
};

function segmentAttributes(segment: ChromeTextSegment): number {
	return createTextAttributes({
		bold: segment.style?.bold,
		dim: segment.style?.dim,
		italic: segment.style?.italic,
		underline: segment.style?.underline,
		strikethrough: segment.style?.strikethrough,
	});
}

function segmentProps(segment: ChromeTextSegment): Record<string, unknown> {
	return {
		fg: segment.style?.fg,
		bg: segment.style?.bg,
		attributes: segmentAttributes(segment),
	};
}

function handleClick(contribution: ChromeContribution) {
	void contribution.onClick?.();
}

function ChromeContributionText(props: {
	contribution: ChromeContribution;
	fg?: string;
}) {
	return (
		<text
			fg={props.fg}
			onMouseUp={
				props.contribution.onClick
					? () => handleClick(props.contribution)
					: undefined
			}
		>
			<For each={props.contribution.content}>
				{(segment) => <span {...segmentProps(segment)}>{segment.text}</span>}
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
				flexWrap="wrap"
				maxWidth="100%"
				overflow="hidden"
			>
				<For each={props.contributions}>
					{(contribution, index) => (
						<>
							<Show when={index() > 0}>
								<text fg={props.separatorFg ?? theme.textMuted}>
									{` ${MIDDLE_DOT} `}
								</text>
							</Show>
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
