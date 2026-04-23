import "../runtime/custom-messages";
import type {
	AgentMessage,
	CustomAgentMessages,
} from "@mariozechner/pi-agent-core";
import type {
	AssistantMessage,
	ToolCall,
	ToolResultMessage,
	UserMessage,
} from "@mariozechner/pi-ai";
import type { BorderSides } from "@opentui/core";
import { TextAttributes } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { createSignal, For, onCleanup, Show } from "solid-js";
import type {
	CodeReviewMessagePart,
	ImageMessagePart,
	MessagePart,
	UserMultipartMessage,
} from "../messages/parts";
import type { Turn } from "../session/types";
import { syntaxStyle, theme } from "./theme";

type BashExecutionMessage = CustomAgentMessages["bashExecution"];

const ABORTED_ATTRS = TextAttributes.DIM | TextAttributes.STRIKETHROUGH;

export type TranscriptPaneProps = {
	turns: Turn[];
};

export type TranscriptTurn = {
	id: string;
	user: (UserMessage | UserMultipartMessage) | null;
	entries: AgentMessage[];
	toolResults: Map<string, ToolResultMessage>;
	aborted: boolean;
};

function toTranscriptTurn(turn: Turn): TranscriptTurn {
	let user: (UserMessage | UserMultipartMessage) | null = null;
	const entries: AgentMessage[] = [];
	const toolResults = new Map<string, ToolResultMessage>();
	let aborted = false;

	for (const msg of turn.messages) {
		if (!("role" in msg)) continue;
		if (msg.role === "user" && user === null) {
			user = msg as UserMessage | UserMultipartMessage;
			continue;
		}

		entries.push(msg);

		if (msg.role === "toolResult") {
			toolResults.set(msg.toolCallId, msg as ToolResultMessage);
		}

		if (
			msg.role === "assistant" &&
			(msg as AssistantMessage).stopReason === "aborted"
		) {
			aborted = true;
		}
	}

	return { id: turn.id, user, entries, toolResults, aborted };
}

function getUserParts(msg: UserMessage | UserMultipartMessage): MessagePart[] {
	if (typeof msg.content === "string") {
		return [{ type: "text", text: msg.content }];
	}
	return msg.content as MessagePart[];
}

function extractUserText(msg: UserMessage | UserMultipartMessage): string {
	return getUserParts(msg)
		.filter(
			(part): part is { type: "text"; text: string } =>
				part.type === "text" && "text" in part && typeof part.text === "string",
		)
		.map((part) => part.text)
		.join("\n");
}

function extractUserCustomParts(
	msg: UserMessage | UserMultipartMessage,
): MessagePart[] {
	return getUserParts(msg).filter((part) => part.type !== "text");
}

function extractAssistantParts(msg: AssistantMessage): {
	text: string;
	toolCalls: ToolCall[];
} {
	const textParts: string[] = [];
	const toolCalls: ToolCall[] = [];
	for (const block of msg.content) {
		if (block.type === "text" && "text" in block && block.text) {
			textParts.push(block.text);
		} else if (block.type === "toolCall" && "name" in block) {
			toolCalls.push(block as ToolCall);
		}
	}
	return { text: textParts.join("\n\n"), toolCalls };
}

function extractToolResultLines(msg: ToolResultMessage): string[] {
	const lines: string[] = [];
	for (const block of msg.content) {
		if (block.type === "text" && "text" in block && block.text) {
			lines.push(...block.text.split("\n"));
		}
	}
	return lines;
}

function formatToolArgs(args?: Record<string, unknown>): string {
	if (!args) return "";
	if ("command" in args && typeof args.command === "string") {
		return ` ${args.command}`;
	}
	if ("path" in args && typeof args.path === "string") return ` ${args.path}`;
	return "";
}

function isAssistantError(msg: AssistantMessage): boolean {
	return msg.stopReason === "error" && !!msg.errorMessage;
}

const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

function InlineSpinner() {
	const [frame, setFrame] = createSignal(0);
	const timer = setInterval(() => {
		setFrame((f) => (f + 1) % SPINNER_FRAMES.length);
	}, 80);
	onCleanup(() => clearInterval(timer));
	return <text fg={theme.toolText}>{SPINNER_FRAMES[frame()]}</text>;
}

function CodeReviewPartEntry(props: {
	part: CodeReviewMessagePart;
	aborted?: boolean;
}) {
	const review = props.part.review;
	const fileCount = review.files.length;
	const commentCount = review.files.reduce(
		(sum, file) =>
			sum + (file.fileComment.trim().length > 0 ? 1 : 0) + file.ranges.length,
		0,
	);
	const summary = `Code review · ${commentCount} comment${commentCount === 1 ? "" : "s"} · ${fileCount} file${fileCount === 1 ? "" : "s"}`;

	return (
		<box
			border={["left" as BorderSides]}
			borderColor={props.aborted ? theme.textMuted : theme.reviewText}
			paddingLeft={1}
			flexDirection="column"
			gap={0}
			width="100%"
		>
			<text
				fg={props.aborted ? theme.textMuted : theme.reviewText}
				attributes={props.aborted ? ABORTED_ATTRS : undefined}
			>
				🧐 {summary}
			</text>
		</box>
	);
}

function ImagePartEntry(props: { part: ImageMessagePart; aborted?: boolean }) {
	const label = props.part.filename ?? "Image attachment";
	return (
		<box
			border={["left" as BorderSides]}
			borderColor={props.aborted ? theme.textMuted : theme.borderAccent}
			paddingLeft={1}
			flexDirection="column"
			gap={0}
			width="100%"
		>
			<text fg={props.aborted ? theme.textMuted : theme.borderAccent}>
				🖼️ {label}
			</text>
		</box>
	);
}

function UserTextEntry(props: { text: string; aborted?: boolean }) {
	return (
		<box
			border={["left" as BorderSides]}
			borderColor={props.aborted ? theme.textMuted : theme.userBorder}
			paddingLeft={1}
			flexDirection="column"
			gap={0}
			width="100%"
		>
			<markdown
				content={props.text}
				syntaxStyle={syntaxStyle}
				conceal
				fg={props.aborted ? theme.textMuted : theme.textPrimary}
			/>
		</box>
	);
}

function UserEntry(props: {
	msg: UserMessage | UserMultipartMessage;
	aborted?: boolean;
}) {
	const text = extractUserText(props.msg);
	const parts = extractUserCustomParts(props.msg);
	return (
		<box flexDirection="column" gap={1} width="100%">
			<Show when={text.trim().length > 0}>
				<UserTextEntry text={text} aborted={props.aborted} />
			</Show>
			<For each={parts}>
				{(part) => {
					switch (part.type) {
						case "code-review":
							return (
								<CodeReviewPartEntry part={part} aborted={props.aborted} />
							);
						case "image":
							return <ImagePartEntry part={part} aborted={props.aborted} />;
						default:
							return null;
					}
				}}
			</For>
		</box>
	);
}

function BashEntry(props: { msg: BashExecutionMessage }) {
	const [expanded, setExpanded] = createSignal(true);
	const outputLines = () => props.msg.output.split("\n");
	const hasOutput = outputLines().length > 0;
	const prefix = props.msg.cancelled
		? "⊘"
		: props.msg.exitCode === 0
			? "✓"
			: "✗";
	const prefixColor = props.msg.cancelled
		? theme.textMuted
		: props.msg.exitCode === 0
			? theme.toolText
			: theme.errorText;

	const displayLines = () => {
		if (!expanded()) return [];
		if (outputLines().length > 20) {
			return [
				...outputLines().slice(0, 18),
				`  ... (${outputLines().length - 18} more lines)`,
			];
		}
		return outputLines();
	};

	return (
		<box
			border={["left" as BorderSides]}
			borderColor={theme.toolText}
			paddingLeft={1}
			flexDirection="column"
			gap={0}
			width="100%"
		>
			<box
				flexDirection="row"
				gap={1}
				onMouseDown={() => hasOutput && setExpanded(!expanded())}
			>
				<text fg={prefixColor}>{prefix}</text>
				<code
					filetype="bash"
					content={props.msg.command}
					syntaxStyle={syntaxStyle}
					fg={theme.textPrimary}
				/>
				<Show when={hasOutput}>
					<text fg={theme.textMuted}>
						{expanded() ? "▾" : "▸"} {outputLines().length} line
						{outputLines().length === 1 ? "" : "s"}
					</text>
				</Show>
			</box>
			<Show when={expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0}>
					<For each={displayLines()}>
						{(line) => <text fg={theme.textMuted}>{line}</text>}
					</For>
				</box>
			</Show>
		</box>
	);
}

function PendingToolCall(props: { tc: ToolCall; aborted?: boolean }) {
	return (
		<box flexDirection="row" gap={1}>
			<Show
				when={!props.aborted}
				fallback={<text fg={theme.textMuted}>⊘</text>}
			>
				<InlineSpinner />
			</Show>
			<text
				fg={props.aborted ? theme.textMuted : theme.toolText}
				attributes={props.aborted ? ABORTED_ATTRS : undefined}
			>
				{props.tc.name}
				{formatToolArgs(props.tc.arguments)}
			</text>
		</box>
	);
}

function CompletedToolCall(props: {
	tc: ToolCall;
	result: ToolResultMessage;
	aborted?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(false);
	const renderer = useRenderer();
	const lines = extractToolResultLines(props.result);
	const prefix = props.aborted ? "⊘" : props.result.isError ? "✗" : "✓";
	const headerColor = props.aborted
		? theme.textMuted
		: props.result.isError
			? theme.errorText
			: theme.toolText;
	const hasOutput = lines.length > 0;

	const displayLines = () => {
		if (!expanded()) return [];
		if (lines.length > 40) {
			return [...lines.slice(0, 38), `  ... (${lines.length - 38} more lines)`];
		}
		return lines;
	};

	return (
		<box flexDirection="column" gap={0} width="100%">
			<box
				flexDirection="row"
				gap={1}
				onMouseDown={() => {
					if (renderer.getSelection()?.getSelectedText()) return;
					if (hasOutput) setExpanded(!expanded());
				}}
			>
				<text
					fg={headerColor}
					attributes={props.aborted ? ABORTED_ATTRS : undefined}
				>
					{prefix} {props.tc.name}
					{formatToolArgs(props.tc.arguments)}
				</text>
				<Show when={hasOutput && !props.aborted}>
					<text fg={theme.textMuted}>
						{expanded() ? "▾" : "▸"} {lines.length} line
						{lines.length === 1 ? "" : "s"}
					</text>
				</Show>
			</box>
			<Show when={expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0}>
					<For each={displayLines()}>
						{(line) => <text fg={theme.textMuted}>{line}</text>}
					</For>
				</box>
			</Show>
		</box>
	);
}

function AssistantEntry(props: {
	msg: AssistantMessage;
	toolResults: Map<string, ToolResultMessage>;
	aborted?: boolean;
}) {
	if (isAssistantError(props.msg)) {
		return (
			<box paddingLeft={1} flexDirection="column" gap={0} width="100%">
				<text fg={theme.errorText}>{props.msg.errorMessage}</text>
			</box>
		);
	}

	const { text, toolCalls } = extractAssistantParts(props.msg);

	return (
		<box flexDirection="column" gap={0} width="100%">
			<For each={toolCalls}>
				{(tc) => {
					const result = () => props.toolResults.get(tc.id);
					return (
						<Show
							when={result()}
							fallback={<PendingToolCall tc={tc} aborted={props.aborted} />}
						>
							{(r) => (
								<CompletedToolCall
									tc={tc}
									result={r()}
									aborted={props.aborted}
								/>
							)}
						</Show>
					);
				}}
			</For>
			<Show when={text.length > 0}>
				<markdown
					content={text}
					syntaxStyle={syntaxStyle}
					conceal
					fg={props.aborted ? theme.textMuted : theme.textPrimary}
				/>
			</Show>
		</box>
	);
}

function TurnEntryItem(props: {
	msg: AgentMessage;
	toolResults: Map<string, ToolResultMessage>;
	aborted: boolean;
}) {
	if (!("role" in props.msg)) return null;

	const role = props.msg.role as string;
	switch (role) {
		case "assistant":
			return (
				<AssistantEntry
					msg={props.msg as AssistantMessage}
					toolResults={props.toolResults}
					aborted={props.aborted}
				/>
			);
		case "bashExecution":
			return <BashEntry msg={props.msg as unknown as BashExecutionMessage} />;
		default:
			return null;
	}
}

function TurnEntry(props: { turn: TranscriptTurn }) {
	return (
		<box flexDirection="column" gap={1} width="100%">
			<Show when={props.turn.user}>
				{(user) => <UserEntry msg={user()} aborted={props.turn.aborted} />}
			</Show>
			<For
				each={props.turn.entries.filter(
					(m) => "role" in m && m.role !== "toolResult",
				)}
			>
				{(msg) => (
					<TurnEntryItem
						msg={msg}
						toolResults={props.turn.toolResults}
						aborted={props.turn.aborted}
					/>
				)}
			</For>
		</box>
	);
}

export function TranscriptPane(props: TranscriptPaneProps) {
	const turns = () => props.turns.map(toTranscriptTurn);

	return (
		<scrollbox
			flexGrow={1}
			height="100%"
			scrollY
			stickyStart="bottom"
			stickyScroll
			padding={1}
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
				<Show when={props.turns.length === 0}>
					<box flexDirection="column" gap={0} width="100%">
						<text fg={theme.textSecondary}>kit</text>
						<text fg={theme.textSecondary}>Start a conversation below.</text>
					</box>
				</Show>
				<For each={turns()}>{(turn) => <TurnEntry turn={turn} />}</For>
			</box>
		</scrollbox>
	);
}
