import {
	createEffect,
	createMemo,
	createSignal,
	onCleanup,
	Show,
} from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import {
	activateChromeContributionAtX,
	ChromeContributionLine,
} from "./ChromeContributionLine";
import {
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

const PRIMARY_MOUSE_BUTTON = 0;

function progressColor(pct: number): string {
	if (pct > 90) return theme.progressCritical;
	if (pct >= 80) return theme.progressWarning;
	return theme.progressNormal;
}

export const HEADER_CONTRIBUTION_IDS = {
	title: "kit.header.title",
	model: "kit.header.model",
} as const;

export type HeaderBarProps = {
	sessionName: string | undefined;
	shellWidth: number;
	transcriptWidth: number;
	showContextProgress?: boolean;
	onHeightChange?: (height: number) => void;
	onOpenOverflow: (contributions: readonly ChromeContribution[]) => void;
	onOverflowAvailabilityChange?: (available: boolean) => void;
	runtime: AgentRuntime;
	header: HeaderStatusController;
};

function contribution(input: {
	id: string;
	content: ChromeContribution["content"];
	side: "left" | "right";
}): ChromeContribution {
	const plainText = input.content.map((segment) => segment.text).join("");
	return {
		id: input.id,
		content: input.content,
		plainText,
		side: input.side,
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
		});
	});
	const modelContribution = createMemo<ChromeContribution>(() => {
		const thinking = agentInfo().thinkingLevel;
		const maxWidth = Math.max(
			5,
			Math.min(42, Math.floor(availableWidth() * 0.38)),
		);
		const separator = ` ${MIDDLE_DOT} `;
		const thinkingWidth = Math.max(
			1,
			Math.min(
				terminalTextWidth(thinking),
				Math.floor((maxWidth - terminalTextWidth(separator)) / 2),
			),
		);
		const compactThinking = truncateEnd(thinking, thinkingWidth);
		const modelWidth = Math.max(
			1,
			maxWidth -
				terminalTextWidth(separator) -
				terminalTextWidth(compactThinking),
		);
		const model = truncateEnd(agentInfo().model?.name ?? "model?", modelWidth);
		return contribution({
			id: HEADER_CONTRIBUTION_IDS.model,
			content: createChromeTextContent(
				`${model}${separator}${compactThinking}`,
			),
			side: "right",
		});
	});
	const privileged = createMemo(() => {
		// Header controller notifications also cover hide/show changes for the
		// built-ins, whose content is assembled locally rather than stored there.
		const standardCount = headerContributions().length;
		const title = props.header.isHidden(HEADER_CONTRIBUTION_IDS.title)
			? null
			: titleContribution();
		const model = props.header.isHidden(HEADER_CONTRIBUTION_IDS.model)
			? null
			: modelContribution();
		let items = [title, model].filter(
			(item): item is ChromeContribution => item !== null,
		);
		if (chromeLayoutWidth(items) > availableWidth() && model) {
			items = items.filter((item) => item.id !== model.id);
		}
		if (standardCount > 0) {
			const overflowProbe: ChromeContribution = {
				id: "HeaderBar:overflow-probe",
				content: [],
				plainText: "…",
				side: "right",
			};
			if (chromeLayoutWidth([...items, overflowProbe]) > availableWidth()) {
				items = items.filter((item) => item.id !== model?.id);
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
