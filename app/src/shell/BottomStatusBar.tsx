import { createEffect, createMemo, createSignal, onCleanup } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import {
	activateChromeContributionAtX,
	ChromeContributionLine,
} from "./ChromeContributionLine";
import type { ComposerInputMode } from "./ComposerDock";
import {
	BUILT_IN_CHROME_CONTRIBUTION_IDS,
	type ChromeContribution,
	createChromeTextContent,
	normalizeChromeTextContent,
} from "./chrome-contributions";
import {
	chromeLayoutWidth,
	packChromeContributions,
	truncateEnd,
	truncateStart,
} from "./chrome-layout";
import type { FooterStatusController } from "./footer-status";
import { MIDDLE_DOT } from "./glyphs";
import { theme } from "./theme";

const PRIMARY_MOUSE_BUTTON = 0;

export const VCS_LOCATION_CONTRIBUTION_ID =
	BUILT_IN_CHROME_CONTRIBUTION_IDS.footerLocation;

export type BottomStatusBarProps = {
	runtime: AgentRuntime;
	status: FooterStatusController;
	composerMode: ComposerInputMode;
	shellWidth: number;
	onOpenOverflow: (contributions: readonly ChromeContribution[]) => void;
	restoreQueueBinding?: string;
	onOverflowAvailabilityChange?: (available: boolean) => void;
};

function contribution(input: {
	id: string;
	content: ChromeContribution["content"];
	side: "left" | "right";
	action?: ChromeContribution["action"];
	onClick?: ChromeContribution["onClick"];
}): ChromeContribution {
	return {
		id: input.id,
		content: input.content,
		plainText: input.content.map((segment) => segment.text).join(""),
		side: input.side,
		action: input.action,
		onClick: input.onClick,
	};
}

export function BottomStatusBar(props: BottomStatusBarProps) {
	const [pendingMessageCount, setPendingMessageCount] = createSignal(
		props.runtime.getPendingMessageCount(),
	);
	const unsubscribePendingMessageCount = props.runtime.subscribe(
		"chat.message-queue.changed",
		(event) => setPendingMessageCount(event.count),
	);
	const [footerContributions, setFooterContributions] = createSignal(
		props.status.getContributions(),
	);
	const unsubscribeStatus = props.status.subscribe(() =>
		setFooterContributions(props.status.getContributions()),
	);

	const availableWidth = () => Math.max(0, props.shellWidth - 4);
	const standard = createMemo(() =>
		footerContributions().filter(
			(item) => item.id !== VCS_LOCATION_CONTRIBUTION_ID,
		),
	);
	const rawGuidance = createMemo<ChromeContribution | null>(() => {
		let label = "";
		let color = theme.textMuted;
		switch (props.composerMode) {
			case "bash":
				label = ["bash command", "result will be added to context"].join(
					` ${MIDDLE_DOT} `,
				);
				color = theme.composerBashBorder;
				break;
			case "bash-excluded":
				label = ["bash command", "result excluded from context"].join(
					` ${MIDDLE_DOT} `,
				);
				color = theme.composerBashExcludedBorder;
				break;
			default:
				if (pendingMessageCount() > 0) {
					label = `queued messages: ${pendingMessageCount()}`;
					const binding = props.restoreQueueBinding;
					if (binding) label += ` ${MIDDLE_DOT} ${binding} restore`;
				}
		}
		if (!label) return null;
		return contribution({
			id: "BottomStatusBar:status",
			content: normalizeChromeTextContent({
				text: label,
				style: { fg: color },
			}),
			side: "left",
		});
	});
	const rawVcsContribution = createMemo<ChromeContribution | null>(() => {
		return (
			footerContributions().find(
				(item) => item.id === VCS_LOCATION_CONTRIBUTION_ID,
			) ?? null
		);
	});
	const needsCompaction = createMemo(() => {
		const privilegedItems = [rawGuidance(), rawVcsContribution()].filter(
			(item): item is ChromeContribution => item !== null,
		);
		if (standard().length === 0) {
			return chromeLayoutWidth(privilegedItems) > availableWidth();
		}
		const overflowProbe: ChromeContribution = {
			id: "BottomStatusBar:overflow-probe",
			content: [],
			plainText: "…",
			side: "right",
		};
		return (
			chromeLayoutWidth([...privilegedItems, overflowProbe]) > availableWidth()
		);
	});
	const guidance = createMemo<ChromeContribution | null>(() => {
		const raw = rawGuidance();
		if (!raw || !needsCompaction()) return raw;
		const compactLabel = truncateEnd(
			raw.plainText,
			Math.max(
				1,
				Math.floor(availableWidth() * (standard().length > 0 ? 0.36 : 0.52)),
			),
		);
		return contribution({
			id: raw.id,
			content: normalizeChromeTextContent({
				text: compactLabel,
				style: raw.content[0]?.style,
			}),
			side: "left",
		});
	});
	const vcsContribution = createMemo<ChromeContribution | null>(() => {
		const raw = rawVcsContribution();
		if (!raw || !needsCompaction()) return raw;
		const standardItemsPresent = standard().length > 0;
		const fraction = standardItemsPresent ? 0.24 : 0.42;
		const maxWidth = Math.max(
			1,
			Math.min(64, Math.floor(availableWidth() * fraction)),
		);
		return contribution({
			id: raw.id,
			content: createChromeTextContent(truncateStart(raw.plainText, maxWidth)),
			side: "right",
			action: raw.action,
			onClick: raw.onClick,
		});
	});
	const privileged = createMemo(() => [
		...(guidance() ? [guidance() as ChromeContribution] : []),
		...(vcsContribution() ? [vcsContribution() as ChromeContribution] : []),
	]);
	const packed = createMemo(() =>
		packChromeContributions({
			availableWidth: availableWidth(),
			privileged: privileged(),
			standard: standard(),
		}),
	);
	createEffect(() => {
		props.onOverflowAvailabilityChange?.(packed().hidden.length > 0);
	});
	const overflowContribution = createMemo<ChromeContribution | null>(() => {
		const label = packed().overflowLabel;
		if (!label) return null;
		return {
			id: "BottomStatusBar:overflow",
			content: createChromeTextContent(label),
			plainText: label,
			side: "right",
			onClick: () => props.onOpenOverflow(standard()),
		};
	});
	const displayed = (side: "left" | "right") => [
		...privileged().filter((item) => item.side === side),
		...packed().visible.filter((item) => item.side === side),
		...(side === "right" && overflowContribution()
			? [overflowContribution() as ChromeContribution]
			: []),
	];

	onCleanup(() => {
		unsubscribePendingMessageCount();
		unsubscribeStatus();
	});

	return (
		<box
			flexShrink={0}
			height={2}
			onMouseDown={(event) => {
				if (event.button !== PRIMARY_MOUSE_BUTTON) return;
				if (
					!activateChromeContributionAtX({
						left: displayed("left"),
						right: displayed("right"),
						shellWidth: props.shellWidth,
						x: event.x,
					})
				)
					return;
				event.preventDefault();
				event.stopPropagation();
			}}
			border={["top"]}
			borderColor={theme.borderStatus}
			paddingX={1}
			width="100%"
			flexDirection="row"
			flexWrap="no-wrap"
			justifyContent="space-between"
			gap={
				displayed("left").length > 0 && displayed("right").length > 0 ? 1 : 0
			}
			overflow="hidden"
		>
			<box flexShrink={1} maxWidth="100%" overflow="hidden">
				<ChromeContributionLine
					contributions={displayed("left")}
					fg={theme.textMuted}
					fallback=""
					wrap={false}
				/>
			</box>
			<box
				flexShrink={0}
				maxWidth="100%"
				overflow="hidden"
				justifyContent="flex-end"
			>
				<ChromeContributionLine
					contributions={displayed("right")}
					fg={theme.textMuted}
					fallback=""
					wrap={false}
				/>
			</box>
		</box>
	);
}
