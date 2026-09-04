/**
 * Shared presentation pieces for sub-agent status surfaces.
 *
 * Used by both the workspace panel and the legacy status modal so the
 * roster rows, transcript rendering, and status vocabulary stay
 * consistent across presentations.
 */

import type { Renderable } from "@opentui/core";
import { createMemo, For, Show } from "solid-js";
import type { ToolResultMessage } from "../../runtime/agent";
import type { SessionEntry, Turn } from "../../session";
import {
	CIRCLE_EMPTY,
	CIRCLE_FILLED,
	CIRCLE_SLASH,
	CROSS,
	HEAVY_LINE,
} from "../../shell/glyphs";
import { scrollbarStyle, theme } from "../../shell/theme";
import { FlatAssistantEntry } from "../../shell/transcript/assistant-entry";
import type { OpenImage } from "../../shell/transcript/types";
import { UserEntry } from "../../shell/transcript/user-entry";
import type { LiveToolsForTurn } from "../../shell/transcript-live-tools";
import type { SubagentDefinition } from "./discovery";
import {
	type ActiveSubagentConversationState,
	type ActiveSubagentStatus,
	buildSubagentTranscriptTurns,
} from "./state";

export type SubagentDisplayStatus = ActiveSubagentStatus | "inactive";

export type SubagentListItem = {
	name: string;
	description: string;
	model?: string;
	source?: SubagentDefinition["source"];
	pluginName?: string;
	status: SubagentDisplayStatus;
	lastActivityAt?: string;
	conversation?: ActiveSubagentConversationState;
};

const STATUS_RANK: Record<SubagentDisplayStatus, number> = {
	running: 0,
	failed: 1,
	aborted: 2,
	idle: 3,
	inactive: 4,
};

export function statusLabel(status: SubagentDisplayStatus): string {
	if (status === "idle") return "completed";
	if (status === "inactive") return "available";
	return status;
}

export function statusIndicator(status: SubagentDisplayStatus): {
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

export function sourceLabel(
	item: Pick<SubagentListItem, "source" | "pluginName">,
) {
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

export function relativeTime(iso: string | undefined): string {
	if (!iso) return "";
	const timestamp = new Date(iso).getTime();
	if (Number.isNaN(timestamp)) return "";
	const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
	if (seconds < 60) return "just now";
	if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
	if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
	return `${Math.floor(seconds / 86400)}d ago`;
}

export function mergeItems(
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
		const conversation = activeByName.get(agent.name);
		return {
			name: agent.name,
			description: agent.description,
			model: conversation?.model ?? agent.model,
			source: agent.source,
			pluginName: agent.pluginName,
			status: conversation?.status ?? "inactive",
			lastActivityAt: conversation?.lastActivityAt,
			conversation,
		};
	});
	const agentNames = new Set(agents.map((agent) => agent.name));
	for (const conversation of activeConversations) {
		if (agentNames.has(conversation.agentName)) continue;
		items.push({
			name: conversation.agentName,
			description:
				conversation.description ?? "Previously active sub-agent conversation",
			model: conversation.model,
			status: conversation.status,
			lastActivityAt: conversation.lastActivityAt,
			conversation,
		});
	}
	return items.sort((a, b) => {
		const rank = STATUS_RANK[a.status] - STATUS_RANK[b.status];
		return rank !== 0 ? rank : a.name.localeCompare(b.name);
	});
}

function toolResultsForTurn(turn: Turn): Map<string, ToolResultMessage> {
	const results = new Map<string, ToolResultMessage>();
	for (const message of turn.messages) {
		if (message.role === "toolResult") results.set(message.toolCallId, message);
	}
	return results;
}

function liveToolsFor(
	conversation: ActiveSubagentConversationState,
): LiveToolsForTurn {
	return Object.fromEntries(
		Object.entries(conversation.liveTools ?? {}).map(([id, tool]) => [
			id,
			{ ...tool, turnId: "live" },
		]),
	);
}

export function SubagentTranscriptView(props: {
	conversation: ActiveSubagentConversationState;
	entries: SessionEntry[];
	openImage: OpenImage;
	setScrollRef: (
		ref: Renderable & {
			scrollBy: (opts: { x: number; y: number }) => void;
			scrollTo: (opts: { x?: number; y?: number } | number) => void;
		},
	) => void;
}) {
	const turns = createMemo(() =>
		buildSubagentTranscriptTurns(
			props.entries,
			props.conversation.subagentConversationId,
		),
	);
	return (
		<scrollbox
			ref={(element) =>
				props.setScrollRef(element as Parameters<typeof props.setScrollRef>[0])
			}
			flexGrow={1}
			scrollY
			stickyStart="bottom"
			stickyScroll
			style={scrollbarStyle()}
		>
			<box flexDirection="column" gap={1} paddingX={1} width="100%">
				<For each={turns()}>
					{(turn) => {
						const results = toolResultsForTurn(turn);
						return (
							<For each={turn.messages}>
								{(message) => {
									if (message.role === "user") {
										return (
											<UserEntry
												itemId={message.messageId}
												msg={message}
												openImage={props.openImage}
											/>
										);
									}
									if (message.role === "assistant") {
										return (
											<FlatAssistantEntry
												msg={message}
												toolResults={results}
												liveTools={{}}
												fullArgs
											/>
										);
									}
									return null;
								}}
							</For>
						);
					}}
				</For>
				<Show when={turns().length === 0 && !props.conversation.liveMessage}>
					<text fg={theme.textMuted}>Waiting for sub-agent activity…</text>
				</Show>
				<Show when={props.conversation.liveMessage}>
					{(message) => (
						<FlatAssistantEntry
							msg={message()}
							toolResults={new Map()}
							liveTools={liveToolsFor(props.conversation)}
							fullArgs
						/>
					)}
				</Show>
				<Show when={props.conversation.status === "failed"}>
					<text fg={theme.errorText}>
						{CROSS} {props.conversation.failureMessage ?? "Sub-agent failed"}
					</text>
				</Show>
				<Show when={props.conversation.status === "aborted"}>
					<text fg={theme.warningText}>
						{CIRCLE_SLASH}{" "}
						{props.conversation.abortReason ?? "Sub-agent aborted"}
					</text>
				</Show>
			</box>
		</scrollbox>
	);
}

export function SubagentsEmptyState() {
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
