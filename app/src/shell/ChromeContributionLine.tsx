import { For, Show } from "solid-js";
import type {
	ChromeContribution,
	ChromeTextStyle,
} from "./chrome-contributions";
import { terminalTextWidth } from "./chrome-layout";
import { MIDDLE_DOT } from "./glyphs";
import { theme } from "./theme";

const PRIMARY_MOUSE_BUTTON = 0;

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

function contributionAtX(
	contributions: readonly ChromeContribution[],
	startX: number,
	x: number,
): ChromeContribution | null {
	let cursor = startX;
	for (const contribution of contributions) {
		const end = cursor + terminalTextWidth(contribution.plainText);
		if (x >= cursor && x < end) return contribution;
		cursor = end + terminalTextWidth(` ${MIDDLE_DOT} `);
	}
	return null;
}

export function activateChromeContributionAtX(input: {
	left: readonly ChromeContribution[];
	right: readonly ChromeContribution[];
	shellWidth: number;
	x: number;
}): boolean {
	const rightWidth =
		input.right.reduce(
			(total, contribution) =>
				total + terminalTextWidth(contribution.plainText),
			0,
		) +
		Math.max(0, input.right.length - 1) * terminalTextWidth(` ${MIDDLE_DOT} `);
	const contribution =
		contributionAtX(input.right, input.shellWidth - 1 - rightWidth, input.x) ??
		contributionAtX(input.left, 1, input.x);
	if (!contribution?.onClick) return false;
	handleClick(contribution);
	return true;
}

function focusedSegmentStyle(
	style: ChromeTextStyle | undefined,
): ChromeTextStyle {
	if (!style) return {};
	const {
		fg: _fg,
		bg: _bg,
		fgToken: _fgToken,
		bgToken: _bgToken,
		...attributes
	} = style;
	return attributes;
}

function resolveSegmentStyle(
	style: ChromeTextStyle | undefined,
): ChromeTextStyle {
	if (!style) return {};
	const { fgToken, bgToken, ...resolved } = style;
	return {
		...resolved,
		fg: fgToken ? theme[fgToken] : resolved.fg,
		bg: bgToken ? theme[bgToken] : resolved.bg,
	};
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
							if (event.button !== PRIMARY_MOUSE_BUTTON) return;
							event.preventDefault();
							event.stopPropagation();
							handleClick(props.contribution);
						}
					: undefined
			}
			onMouseUp={
				props.contribution.onClick
					? (event) => {
							if (event.button !== PRIMARY_MOUSE_BUTTON) return;
							event.preventDefault();
							event.stopPropagation();
						}
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
								: resolveSegmentStyle(segment.style)
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
