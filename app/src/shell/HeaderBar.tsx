import {
	createEffect,
	createMemo,
	createSignal,
	onCleanup,
	Show,
} from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { ChromeContributionLine } from "./ChromeContributionLine";
import {
	BUILT_IN_CHROME_CONTRIBUTION_IDS,
	type ChromeContribution,
	createChromeTextContent,
	normalizeChromeTextContent,
} from "./chrome-contributions";
import {
	chromeLayoutWidth,
	packChromeContributions,
	terminalTextWidth,
	transcriptContextProgressColumns,
	truncateEnd,
} from "./chrome-layout";
import { HORIZONTAL_LINE, MIDDLE_DOT } from "./glyphs";
import type { HeaderStatusController } from "./header-status";
import { theme } from "./theme";

function progressColor(pct: number): string {
	if (pct > 90) return theme.progressCritical;
	if (pct >= 80) return theme.progressWarning;
	return theme.progressNormal;
}

export const HEADER_CONTRIBUTION_IDS = {
	title: BUILT_IN_CHROME_CONTRIBUTION_IDS.headerTitle,
	model: BUILT_IN_CHROME_CONTRIBUTION_IDS.headerModel,
	thinking: BUILT_IN_CHROME_CONTRIBUTION_IDS.headerThinking,
} as const;

export type HeaderBarProps = {
	sessionName: string | undefined;
	shellWidth: number;
	transcriptWidth: number;
	showContextProgress?: boolean;
	actionsDisabled?: () => boolean;
	onHeightChange?: (height: number) => void;
	onOpenOverflow: (contributions: readonly ChromeContribution[]) => void;
	onOverflowAvailabilityChange?: (available: boolean) => void;
	onRenameSession?: () => void;
	onSelectModel?: () => void;
	onSelectThinkingLevel?: () => void;
	runtime: AgentRuntime;
	header: HeaderStatusController;
};

function contribution(input: {
	id: string;
	content: ChromeContribution["content"];
	side: "left" | "right";
	onClick?: () => void;
}): ChromeContribution {
	const plainText = input.content.map((segment) => segment.text).join("");
	return {
		id: input.id,
		content: input.content,
		plainText,
		side: input.side,
		onClick: input.onClick,
	};
}

export function HeaderBar(props: HeaderBarProps) {
	let barRef: { height: number } | undefined;

	const [contextStats, setContextStats] = createSignal(
		props.runtime.contextStats,
	);
	const refreshContextStats = () => setContextStats(props.runtime.contextStats);
	const unsubscribeTurns = props.runtime.subscribe(
		"agent.turn.completed",
		refreshContextStats,
	);
	const unsubscribeSessionChange = props.runtime.subscribe(
		"session.active.changed",
		refreshContextStats,
	);
	const unsubscribeCompactionCompleted = props.runtime.subscribe(
		{ prefix: "session.compaction.completed" },
		refreshContextStats,
	);
	const contextUsage = () => contextStats()?.percent ?? 0;

	const [agentInfo, setAgentInfo] = createSignal(props.runtime.agentInfo);
	const unsubscribeAgentInfo = props.runtime.subscribe(
		"agent.model.changed",
		(event) => {
			setAgentInfo(event);
			refreshContextStats();
		},
	);
	const [headerContributions, setHeaderContributions] = createSignal(
		props.header.getContributions(),
	);
	const unsubscribeHeader = props.header.subscribe(() =>
		setHeaderContributions(props.header.getContributions()),
	);

	const availableWidth = () => Math.max(0, props.shellWidth - 4);
	const titleContribution = createMemo<ChromeContribution>(() => {
		const maxWidth = Math.max(
			1,
			Math.min(48, Math.floor(availableWidth() * 0.42)),
		);
		const session = truncateEnd(
			props.sessionName || "Unnamed session",
			maxWidth,
		);
		return contribution({
			id: HEADER_CONTRIBUTION_IDS.title,
			content: normalizeChromeTextContent({
				text: session,
				style: { fg: theme.textPrimary },
			}),
			side: "left",
			onClick: props.onRenameSession,
		});
	});
	const agentConfigurationContributions = createMemo<ChromeContribution[]>(
		() => {
			// Subscribe to controller notifications for built-ins assembled locally.
			void headerContributions();
			const showModel = !props.header.isHidden(HEADER_CONTRIBUTION_IDS.model);
			const showThinking = !props.header.isHidden(
				HEADER_CONTRIBUTION_IDS.thinking,
			);
			if (!showModel && !showThinking) return [];

			const maxWidth = Math.max(
				5,
				Math.min(42, Math.floor(availableWidth() * 0.38)),
			);
			const thinking = agentInfo().thinkingLevel;
			let thinkingWidth = showThinking ? maxWidth : 0;
			let modelWidth = showModel ? maxWidth : 0;
			if (showModel && showThinking) {
				const separatorWidth = terminalTextWidth(` ${MIDDLE_DOT} `);
				thinkingWidth = Math.max(
					1,
					Math.min(
						terminalTextWidth(thinking),
						Math.floor((maxWidth - separatorWidth) / 2),
					),
				);
				modelWidth = Math.max(
					1,
					maxWidth -
						separatorWidth -
						terminalTextWidth(truncateEnd(thinking, thinkingWidth)),
				);
			}

			return [
				...(showModel
					? [
							contribution({
								id: HEADER_CONTRIBUTION_IDS.model,
								content: createChromeTextContent(
									truncateEnd(agentInfo().model?.name ?? "model?", modelWidth),
								),
								side: "right",
								onClick: props.onSelectModel,
							}),
						]
					: []),
				...(showThinking
					? [
							contribution({
								id: HEADER_CONTRIBUTION_IDS.thinking,
								content: createChromeTextContent(
									truncateEnd(thinking, thinkingWidth),
								),
								side: "right",
								onClick: props.onSelectThinkingLevel,
							}),
						]
					: []),
			];
		},
	);
	const privileged = createMemo(() => {
		// Header controller notifications also cover hide/show changes for the
		// built-ins, whose content is assembled locally rather than stored there.
		const standardCount = headerContributions().length;
		const title = props.header.isHidden(HEADER_CONTRIBUTION_IDS.title)
			? null
			: titleContribution();
		const agentConfiguration = agentConfigurationContributions();
		let items = [...(title ? [title] : []), ...agentConfiguration];
		const removeAgentConfiguration = () => {
			const configurationIds = new Set(
				agentConfiguration.map((item) => item.id),
			);
			items = items.filter((item) => !configurationIds.has(item.id));
		};
		if (chromeLayoutWidth(items) > availableWidth()) {
			removeAgentConfiguration();
		}
		if (standardCount > 0) {
			const overflowProbe: ChromeContribution = {
				id: "HeaderBar:overflow-probe",
				content: [],
				plainText: "…",
				side: "right",
			};
			if (chromeLayoutWidth([...items, overflowProbe]) > availableWidth()) {
				removeAgentConfiguration();
			}
			if (chromeLayoutWidth([...items, overflowProbe]) > availableWidth()) {
				items = items.filter((item) => item.id !== title?.id);
			}
		}
		return items;
	});
	const packed = createMemo(() =>
		packChromeContributions({
			availableWidth: availableWidth(),
			privileged: privileged(),
			standard: headerContributions(),
		}),
	);
	createEffect(() => {
		props.onOverflowAvailabilityChange?.(packed().hidden.length > 0);
	});
	const overflowContribution = createMemo<ChromeContribution | null>(() => {
		const label = packed().overflowLabel;
		if (!label) return null;
		return {
			id: "HeaderBar:overflow",
			content: createChromeTextContent(label),
			plainText: label,
			side: "right",
			onClick: () => props.onOpenOverflow(headerContributions()),
		};
	});
	const displayed = (side: "left" | "right") => [
		...privileged().filter((item) => item.side === side),
		...packed().visible.filter((item) => item.side === side),
		...(side === "right" && overflowContribution()
			? [overflowContribution() as ChromeContribution]
			: []),
	];

	const filledProgressColumns = () =>
		props.showContextProgress === false
			? 0
			: transcriptContextProgressColumns({
					shellWidth: props.shellWidth,
					transcriptWidth: props.transcriptWidth,
					percent: contextUsage(),
				});

	onCleanup(() => {
		unsubscribeTurns();
		unsubscribeAgentInfo();
		unsubscribeSessionChange();
		unsubscribeCompactionCompleted();
		unsubscribeHeader();
	});

	return (
		<box
			flexShrink={0}
			height={2}
			ref={(value) => {
				barRef = value as typeof barRef;
			}}
			onSizeChange={() => {
				if (barRef) props.onHeightChange?.(barRef.height);
			}}
		>
			<box
				border={["bottom"]}
				borderColor={theme.borderDefault}
				paddingX={1}
				width="100%"
				height={2}
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
						wrap={false}
						disabled={props.actionsDisabled}
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
						disabled={props.actionsDisabled}
					/>
				</box>
			</box>

			<Show when={filledProgressColumns() > 0}>
				<text
					position="absolute"
					top={1}
					left={0}
					fg={progressColor(contextUsage())}
				>
					{HORIZONTAL_LINE.repeat(filledProgressColumns())}
				</text>
			</Show>
		</box>
	);
}
