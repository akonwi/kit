import { For, Show } from "solid-js";
import type { ChromeContribution } from "./chrome-contributions";
import { MIDDLE_DOT } from "./glyphs";
import { theme } from "./theme";

type ChromeContributionLineProps = {
	contributions: readonly ChromeContribution[];
	fg?: string;
	separatorFg?: string;
	fallback?: string;
};

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
				{(segment) => (
					// The Solid reconciler only applies span styling through the
					// `style` object prop; direct fg/bg/attributes props are
					// silently ignored on text nodes.
					<span style={segment.style ?? {}}>{segment.text}</span>
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
