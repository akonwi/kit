/** @jsxImportSource solid-js */
import { For, type JSX, Show } from "solid-js";
import { MIDDLE_DOT } from "../shell/glyphs";
import {
	CHROME_TOKEN_COLORS,
	type RemoteChromeContribution,
	type RemoteChromeTextStyle,
} from "./chrome-state";

function segmentStyle(
	style: RemoteChromeTextStyle | undefined,
): JSX.CSSProperties {
	if (!style) return {};
	const decorations = [
		...(style.underline ? ["underline"] : []),
		...(style.strikethrough ? ["line-through"] : []),
	].join(" ");
	return {
		...(style.fgToken ? { color: CHROME_TOKEN_COLORS[style.fgToken] } : {}),
		...(style.bgToken
			? { "background-color": CHROME_TOKEN_COLORS[style.bgToken] }
			: {}),
		...(style.bold ? { "font-weight": 700 } : {}),
		...(style.dim ? { opacity: 0.65 } : {}),
		...(style.italic ? { "font-style": "italic" } : {}),
		...(decorations ? { "text-decoration": decorations } : {}),
	};
}

function RemoteChromeContributionView(props: {
	area: "header" | "footer";
	contribution: RemoteChromeContribution;
	disabled: boolean;
	onActivate: (area: "header" | "footer", contributionId: string) => void;
}): JSX.Element {
	const content = () => (
		<For each={props.contribution.content}>
			{(segment) => (
				<span style={segmentStyle(segment.style)}>{segment.text}</span>
			)}
		</For>
	);
	return (
		<Show
			when={props.contribution.action}
			fallback={
				<Show
					when={props.contribution.clickable}
					fallback={
						<span
							class="remote-chrome-contribution"
							title={props.contribution.plainText}
						>
							{content()}
						</span>
					}
				>
					<button
						class="remote-chrome-contribution is-clickable"
						type="button"
						disabled={props.disabled}
						title={props.contribution.plainText}
						onClick={() => props.onActivate(props.area, props.contribution.id)}
					>
						{content()}
					</button>
				</Show>
			}
		>
			{(action) => (
				<a
					class="remote-chrome-contribution is-link"
					href={action().url}
					target="_blank"
					rel="noopener noreferrer"
					aria-label={`${props.contribution.plainText} (opens in new tab)`}
					title={props.contribution.plainText}
				>
					{content()}
				</a>
			)}
		</Show>
	);
}

export function RemoteChromeLine(props: {
	area: "header" | "footer";
	contributions: RemoteChromeContribution[];
	side: "left" | "right";
	leadingSeparator?: boolean;
	disabled: boolean;
	onActivate: (area: "header" | "footer", contributionId: string) => void;
}): JSX.Element {
	const contributions = () =>
		props.contributions.filter(
			(contribution) => contribution.side === props.side,
		);
	return (
		<>
			<Show when={props.leadingSeparator && contributions().length > 0}>
				<span class="chrome-separator" aria-hidden="true">
					{MIDDLE_DOT}
				</span>
			</Show>
			<For each={contributions()}>
				{(contribution, index) => (
					<>
						<Show when={index() > 0}>
							<span class="chrome-separator" aria-hidden="true">
								{MIDDLE_DOT}
							</span>
						</Show>
						<RemoteChromeContributionView
							area={props.area}
							contribution={contribution}
							disabled={props.disabled}
							onActivate={props.onActivate}
						/>
					</>
				)}
			</For>
		</>
	);
}
