import { useRenderer } from "@opentui/solid";
import { createEffect, For, onCleanup, Show } from "solid-js";
import type {
	ChromeContribution,
	ChromeTextStyle,
} from "./chrome-contributions";
import { terminalTextWidth } from "./chrome-layout";
import { MIDDLE_DOT } from "./glyphs";
import { openExternal } from "./open-external";
import { theme } from "./theme";

const PRIMARY_MOUSE_BUTTON = 0;

type OpenUrl = (url: string) => void | Promise<void>;

type ChromeContributionLineProps = {
	contributions: readonly ChromeContribution[];
	fg?: string;
	separatorFg?: string;
	fallback?: string;
	wrap?: boolean;
	disabled?: boolean | (() => boolean);
	delegateActivation?: boolean;
	onOpenUrl?: OpenUrl;
};

function isActionable(contribution: ChromeContribution): boolean {
	return (
		contribution.onClick !== undefined || contribution.action !== undefined
	);
}

export async function activateChromeContribution(
	contribution: ChromeContribution,
	onOpenUrl: OpenUrl = openExternal,
): Promise<boolean> {
	if (contribution.action?.type === "open-url") {
		await onOpenUrl(contribution.action.url);
		return true;
	}
	if (!contribution.onClick) return false;
	await contribution.onClick();
	return true;
}

function handleClick(
	contribution: ChromeContribution,
	onOpenUrl: OpenUrl = openExternal,
) {
	void activateChromeContribution(contribution, onOpenUrl).catch((error) => {
		console.error("Could not activate chrome contribution:", error);
	});
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

export function actionableChromeContributionAtX(input: {
	left: readonly ChromeContribution[];
	right: readonly ChromeContribution[];
	shellWidth: number;
	x: number;
}): ChromeContribution | null {
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
	return contribution && isActionable(contribution) ? contribution : null;
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
	disabled?: boolean | (() => boolean);
	delegateActivation?: boolean;
	onOpenUrl?: OpenUrl;
}) {
	const renderer = useRenderer();
	const actionable = isActionable(props.contribution);
	const isDisabled = () =>
		typeof props.disabled === "function" ? props.disabled() : props.disabled;
	const isEnabled = () => !isDisabled() && actionable;
	const handlesActivation = actionable && !props.delegateActivation;
	let hovered = false;
	let pressed = false;
	createEffect(() => {
		if (isEnabled()) return;
		pressed = false;
		if (!hovered) return;
		hovered = false;
		renderer.setMousePointer("default");
	});
	onCleanup(() => {
		if (hovered) renderer.setMousePointer("default");
	});

	return (
		<text
			fg={props.focused ? theme.pickerFocusedText : props.fg}
			bg={props.focused ? theme.pickerFocusedBg : undefined}
			onMouseDown={
				handlesActivation
					? (event) => {
							pressed = isEnabled() && event.button === PRIMARY_MOUSE_BUTTON;
						}
					: undefined
			}
			onMouseDrag={
				handlesActivation
					? () => {
							pressed = false;
						}
					: undefined
			}
			onMouseUp={
				handlesActivation
					? (event) => {
							const shouldActivate = pressed && isEnabled();
							pressed = false;
							if (!shouldActivate) return;
							event.preventDefault();
							event.stopPropagation();
							handleClick(props.contribution, props.onOpenUrl);
						}
					: undefined
			}
			onMouseOver={
				actionable
					? () => {
							if (!isEnabled()) return;
							hovered = true;
							renderer.setMousePointer("pointer");
						}
					: undefined
			}
			onMouseOut={
				actionable
					? () => {
							hovered = false;
							renderer.setMousePointer("default");
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
								disabled={props.disabled}
								delegateActivation={props.delegateActivation}
								onOpenUrl={props.onOpenUrl}
							/>
						</>
					)}
				</For>
			</box>
		</Show>
	);
}
