import type { Renderable } from "@opentui/core";
import { createMemo, createSignal, For, onCleanup, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { Dialog } from "../../shell/Dialog";
import {
	CIRCLE_EMPTY,
	CIRCLE_FILLED,
	CIRCLE_SLASH,
	CROSS,
	HEAVY_LINE,
	MIDDLE_DOT,
} from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import type { SubagentDefinition } from "./discovery";
import type {
	ActiveSubagentConversationState,
	ActiveSubagentStatus,
} from "./state";

type SubagentDisplayStatus = ActiveSubagentStatus | "inactive";

type SubagentListItem = {
	name: string;
	description: string;
	model?: string;
	source?: SubagentDefinition["source"];
	pluginName?: string;
	status: SubagentDisplayStatus;
	lastActivityAt?: string;
};

export type SubagentsStatusModalProps = {
	surfaceProps?: OverlaySurfaceProps;
	getAgents: () => SubagentDefinition[];
	getActiveConversations: () => ActiveSubagentConversationState[];
	active?: boolean;
	onClose: () => void;
};

const STATUS_RANK: Record<SubagentDisplayStatus, number> = {
	running: 0,
	failed: 1,
	aborted: 2,
	idle: 3,
	inactive: 4,
};

function statusIndicator(status: SubagentDisplayStatus): {
	glyph: string;
	color: string;
} {
	switch (status) {
		case "running":
			return { glyph: CIRCLE_FILLED, color: theme.subagentText };
		case "failed":
			return { glyph: CROSS, color: theme.errorText };
		case "aborted":
			return { glyph: CIRCLE_SLASH, color: theme.warningText };
		case "idle":
			return { glyph: CIRCLE_EMPTY, color: theme.textSecondary };
		case "inactive":
			return { glyph: CIRCLE_EMPTY, color: theme.textMuted };
	}
}

function sourceLabel(
	item: Pick<SubagentListItem, "source" | "pluginName">,
): string {
	switch (item.source) {
		case "kit-user":
			return "user";
		case "kit-project":
			return "project";
		case "plugin":
			return item.pluginName ? `plugin:${item.pluginName}` : "plugin";
		case undefined:
			return "active";
	}
}

function relativeTime(iso: string | undefined): string {
	if (!iso) return "";
	const timestamp = new Date(iso).getTime();
	if (Number.isNaN(timestamp)) return "";
	const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
	if (seconds < 60) return "just now";
	if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
	if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
	return `${Math.floor(seconds / 86400)}d ago`;
}

function mergeItems(
	agents: SubagentDefinition[],
	activeConversations: ActiveSubagentConversationState[],
): SubagentListItem[] {
	const activeByName = new Map(
		activeConversations.map((conversation) => [
			conversation.agentName,
			conversation,
		]),
	);
	const items = agents.map<SubagentListItem>((agent) => {
		const active = activeByName.get(agent.name);
		const status: SubagentDisplayStatus = active?.status ?? "inactive";
		return {
			name: agent.name,
			description: agent.description,
			model: active?.model ?? agent.model,
			source: agent.source,
			pluginName: agent.pluginName,
			status,
			lastActivityAt: active?.lastActivityAt,
		};
	});
	const agentNames = new Set(agents.map((agent) => agent.name));
	for (const active of activeConversations) {
		if (agentNames.has(active.agentName)) continue;
		items.push({
			name: active.agentName,
			description:
				active.description ?? "Previously active sub-agent conversation",
			model: active.model,
			status: active.status,
			lastActivityAt: active.lastActivityAt,
		});
	}
	return items.sort((a, b) => {
		const rank = STATUS_RANK[a.status] - STATUS_RANK[b.status];
		if (rank !== 0) return rank;
		return a.name.localeCompare(b.name);
	});
}

function SubagentRow(props: { item: SubagentListItem }) {
	const indicator = () => statusIndicator(props.item.status);
	const detail = () => {
		const source = sourceLabel(props.item);
		return props.item.model
			? `${props.item.model} ${MIDDLE_DOT} ${source}`
			: source;
	};
	const lastActivity = () => relativeTime(props.item.lastActivityAt);

	return (
		<box flexDirection="column" gap={0}>
			<box flexDirection="row" justifyContent="space-between" gap={1}>
				<box flexDirection="row" gap={1} flexShrink={1} overflow="hidden">
					<text fg={indicator().color}>{indicator().glyph}</text>
					<text fg={theme.textPrimary} truncate>
						{props.item.name}
					</text>
					<text fg={theme.textMuted}>{MIDDLE_DOT}</text>
					<text fg={indicator().color}>{props.item.status}</text>
					<text fg={theme.textMuted}>{MIDDLE_DOT}</text>
					<text fg={theme.textMuted} truncate>
						{detail()}
					</text>
				</box>
				<Show when={lastActivity()}>
					<text fg={theme.textMuted}>{lastActivity()}</text>
				</Show>
			</box>
			<text fg={theme.textSecondary} paddingLeft={2} truncate>
				{props.item.description}
			</text>
		</box>
	);
}

function EmptyState() {
	return (
		<box
			flexGrow={1}
			flexDirection="column"
			justifyContent="center"
			alignItems="center"
			gap={1}
		>
			<text fg={theme.textPrimary}>k i t</text>
			<text fg={theme.borderAccent}>{HEAVY_LINE.repeat(11)}</text>
			<text fg={theme.textSecondary}>No sub-agents available</text>
			<text fg={theme.textPlaceholder}>
				Add .md files to ~/.kit/agents/ or use plugins
			</text>
		</box>
	);
}

export function SubagentsStatusModal(props: SubagentsStatusModalProps) {
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);
	const [refreshTick, setRefreshTick] = createSignal(0);
	const refreshInterval = setInterval(
		() => setRefreshTick((tick) => tick + 1),
		1000,
	);
	onCleanup(() => clearInterval(refreshInterval));
	const items = createMemo(() => {
		refreshTick();
		return mergeItems(props.getAgents(), props.getActiveConversations());
	});
	const activeCount = createMemo(
		() => items().filter((item) => item.status !== "inactive").length,
	);

	useKeymapLayer(() => ({
		scope: "modal",
		target: rootTarget,
		targetMode: "focus-within",
		when: () => props.active !== false,
		commands: {
			"subagents.close": () => props.onClose(),
		},
	}));

	return (
		<Dialog.Root
			width="70%"
			height="65%"
			maxWidth={120}
			surfaceProps={props.surfaceProps}
			rootRef={setRootTarget}
			rootFocusable
			rootFocused={props.active !== false}
		>
			<Dialog.Header>
				<Dialog.Title>Sub-agents</Dialog.Title>
				<Show when={activeCount() > 0}>
					<Dialog.Meta>{activeCount()} active</Dialog.Meta>
				</Show>
			</Dialog.Header>

			<Dialog.Body>
				<Show when={items().length > 0} fallback={<EmptyState />}>
					<scrollbox
						flexGrow={1}
						scrollY
						focused={props.active !== false}
						style={{
							scrollbarOptions: {
								trackOptions: {
									foregroundColor: theme.scrollbarFg,
									backgroundColor: theme.scrollbarBg,
								},
							},
						}}
					>
						<box flexDirection="column" gap={1} width="100%">
							<For each={items()}>{(item) => <SubagentRow item={item} />}</For>
						</box>
					</scrollbox>
				</Show>
			</Dialog.Body>

			<Dialog.Footer>
				<KeymapHintBar borderless group="subagents" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
