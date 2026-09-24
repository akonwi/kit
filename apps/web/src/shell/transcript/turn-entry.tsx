import { createMemo, For } from "solid-js";
import type { ToolResultMessage } from "../../runtime/agent";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { LiveToolsForTurn } from "../transcript-live-tools";
import { AssistantEntry } from "./assistant-entry";
import { BashEntry } from "./bash-entry";
import { DrawerChip } from "./drawer-chip";
import { HandoffSummaryEntry } from "./handoff-summary-entry";
import {
	extractPresentedToolImage,
	PresentedImage,
	type PresentedToolImage,
} from "./presented-image";
import {
	type DisplayItem,
	extractAssistantParts,
	type TranscriptItem,
} from "./turns";
import type {
	OpenActivity,
	OpenImage,
	OpenMessageContextMenu,
	OpenSubagent,
	TranscriptToast,
} from "./types";
import { UserEntry } from "./user-entry";

const EMPTY_TOOL_CALL_IDS = new Set<string>();
const IGNORE_IMAGE_EXPANSION = () => {};

type PresentedImageEntry = {
	toolCallId: string;
	image: PresentedToolImage;
};

function samePresentedImage(
	left: PresentedToolImage,
	right: PresentedToolImage,
): boolean {
	return (
		left.data === right.data &&
		left.mimeType === right.mimeType &&
		left.path === right.path &&
		left.filename === right.filename &&
		left.caption === right.caption &&
		left.width === right.width &&
		left.height === right.height
	);
}

/**
 * Chip for the consolidated intermediate work of a turn. Clicking opens the
 * turn activity workspace panel, kept live via the runtime.
 */
function TurnWorkDrawer(props: {
	items: TranscriptItem[];
	liveTools: LiveToolsForTurn;
	runtime: AgentRuntime;
	openActivity: OpenActivity;
	openSubagent: OpenSubagent;
	openImage: OpenImage;
	showToast: (toast: TranscriptToast) => void;
	expandedImageToolCallIds: ReadonlySet<string>;
	setImageToolCallExpanded: (toolCallId: string, expanded: boolean) => void;
}) {
	if (props.items.length === 0) return null;

	const turnId = props.items[0].turnId;

	const allToolCalls = createMemo(() =>
		props.items.flatMap((item) =>
			item.kind === "assistant"
				? extractAssistantParts(item.message).toolCalls
				: [],
		),
	);
	const allToolResults = createMemo(() => {
		const merged = new Map<string, ToolResultMessage>();
		for (const item of props.items) {
			if (item.kind === "assistant") {
				for (const [id, result] of item.toolResults) {
					merged.set(id, result);
				}
			}
		}
		return merged;
	});
	const aborted = createMemo(() =>
		props.items.some((item) =>
			item.kind === "assistant" ? item.aborted : false,
		),
	);
	const presentedImages = createMemo<PresentedImageEntry[]>((previous = []) => {
		const previousById = new Map(
			previous.map((entry) => [entry.toolCallId, entry]),
		);
		return allToolCalls().flatMap((toolCall) => {
			const result =
				allToolResults().get(toolCall.id) ??
				props.liveTools[toolCall.id]?.result;
			const image = extractPresentedToolImage(toolCall.name, result);
			if (!image) return [];
			const existing = previousById.get(toolCall.id);
			return existing && samePresentedImage(existing.image, image)
				? [existing]
				: [{ toolCallId: toolCall.id, image }];
		});
	});

	function openActivity() {
		props.openActivity({
			kind: "turn-intermediate",
			turnId,
			anchorItemId: props.items[0]?.id ?? "",
		});
	}

	const stepLabel = createMemo(() => {
		const n = props.items.length;
		return `${n} step${n === 1 ? "" : "s"}`;
	});

	return (
		<box
			flexDirection="column"
			gap={presentedImages().length > 0 ? 1 : 0}
			width="100%"
		>
			<DrawerChip
				toolCalls={allToolCalls()}
				toolResults={allToolResults()}
				aborted={aborted()}
				onActivate={openActivity}
				onOpenSubagent={props.openSubagent}
				emptyLabel={stepLabel()}
			/>
			<For each={presentedImages()}>
				{({ toolCallId, image }) => (
					<PresentedImage
						image={image}
						expanded={props.expandedImageToolCallIds.has(toolCallId)}
						onExpandedChange={(expanded) =>
							props.setImageToolCallExpanded(toolCallId, expanded)
						}
						onOpen={() =>
							props.openImage({
								id: toolCallId,
								image: {
									data: image.data,
									mimeType: image.mimeType,
									filename: image.filename,
									sourcePath: image.path,
									caption: image.caption,
									width: image.width,
									height: image.height,
								},
							})
						}
						aborted={aborted()}
						showToast={props.showToast}
					/>
				)}
			</For>
		</box>
	);
}

export function TurnEntry(props: {
	displayItem: DisplayItem;
	liveTools: LiveToolsForTurn;
	showToast: (toast: TranscriptToast) => void;
	runtime: AgentRuntime;
	openActivity: OpenActivity;
	openSubagent: OpenSubagent;
	openImage: OpenImage;
	openMessageContextMenu: OpenMessageContextMenu;
	expandedImageToolCallIds?: ReadonlySet<string>;
	setImageToolCallExpanded?: (toolCallId: string, expanded: boolean) => void;
}) {
	if (props.displayItem.kind === "turn-work") {
		return (
			<TurnWorkDrawer
				items={props.displayItem.items}
				liveTools={props.liveTools}
				runtime={props.runtime}
				openActivity={props.openActivity}
				openSubagent={props.openSubagent}
				openImage={props.openImage}
				showToast={props.showToast}
				expandedImageToolCallIds={
					props.expandedImageToolCallIds ?? EMPTY_TOOL_CALL_IDS
				}
				setImageToolCallExpanded={
					props.setImageToolCallExpanded ?? IGNORE_IMAGE_EXPANSION
				}
			/>
		);
	}

	if (props.displayItem.kind === "assistant-prose") {
		const item = props.displayItem.item;
		return (
			<AssistantEntry
				itemId={item.id}
				msg={item.message}
				toolResults={item.toolResults}
				liveTools={props.liveTools}
				aborted={item.aborted}
				runtime={props.runtime}
				openActivity={props.openActivity}
				openSubagent={props.openSubagent}
				openMessageContextMenu={props.openMessageContextMenu}
				hideTools
			/>
		);
	}

	const item = props.displayItem.item;
	switch (item.kind) {
		case "user":
			return (
				<UserEntry
					itemId={item.id}
					msg={item.message}
					aborted={item.aborted}
					openImage={props.openImage}
					openMessageContextMenu={props.openMessageContextMenu}
				/>
			);
		case "assistant":
			return (
				<AssistantEntry
					itemId={item.id}
					msg={item.message}
					toolResults={item.toolResults}
					liveTools={props.liveTools}
					aborted={item.aborted}
					runtime={props.runtime}
					openActivity={props.openActivity}
					openSubagent={props.openSubagent}
					openMessageContextMenu={props.openMessageContextMenu}
				/>
			);
		case "handoff-summary":
			return <HandoffSummaryEntry msg={item.message} aborted={item.aborted} />;
		case "bash":
			return <BashEntry msg={item.message} />;
	}
}
