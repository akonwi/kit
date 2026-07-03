import { useBindings } from "@opentui/keymap/solid";
import { createEffect, createSignal, For, onCleanup, Show } from "solid-js";
import type { OverlayComponentProps } from "../app/overlay-ui";
import { withKitKeyAliases } from "../keymap/bindings";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { Dialog } from "./Dialog";
import { CHEVRON_RIGHT, MIDDLE_DOT } from "./glyphs";
import { type Binding, HintBar } from "./HintBar";
import { MessageComposer, type TextareaRef } from "./MessageComposer";
import { scrollbarStyle, theme } from "./theme";

type QueueEditorDialogProps = OverlayComponentProps<void> & {
	runtime: AgentRuntime;
};

type Mode = "list" | "edit" | "confirm-clear";

type QueueScrollRef = {
	scrollChildIntoView: (id: string) => void;
};

const LIST_HINTS: Binding[] = [
	{ key: "↑/↓", action: "move" },
	{ key: "enter", action: "edit" },
	{ key: "d", action: "delete" },
	{ key: "c", action: "clear" },
	{ key: "esc", action: "close" },
];

const EDIT_HINTS: Binding[] = [
	{ key: "enter", action: "save" },
	{ key: "shift+enter", action: "newline" },
	{ key: "esc", action: "cancel" },
];

const CONFIRM_HINTS: Binding[] = [
	{ key: "enter", action: "clear all" },
	{ key: "esc", action: "cancel" },
];

function previewMessage(message: string): string {
	return message.replace(/\s+/g, " ").trim();
}

function messageMeta(message: string): string {
	const lines = message.split(/\r?\n/).length;
	const lineLabel = lines === 1 ? "1 line" : `${lines} lines`;
	const chars = message.length;
	const charLabel = chars === 1 ? "1 char" : `${chars} chars`;
	return `${lineLabel} ${MIDDLE_DOT} ${charLabel}`;
}

export function QueueEditorDialog(props: QueueEditorDialogProps) {
	const [messages, setMessages] = createSignal(
		props.runtime.getPendingMessages(),
	);
	const [selected, setSelected] = createSignal(0);
	const [mode, setMode] = createSignal<Mode>("list");
	const [draft, setDraft] = createSignal("");
	let textareaRef: TextareaRef | undefined;
	let scrollRef: QueueScrollRef | undefined;

	const applyMessages = (next: string[]) => {
		setMessages(next);
		setSelected((current) =>
			Math.min(Math.max(0, current), Math.max(0, next.length - 1)),
		);
		if (next.length === 0) props.done();
	};

	const syncMessages = () => {
		applyMessages(props.runtime.getPendingMessages());
	};

	const unsubscribe = props.runtime.subscribe(
		"chat.message-queue.changed",
		(event) => {
			if (mode() !== "list") {
				setMode("list");
				setDraft("");
				textareaRef = undefined;
			}
			applyMessages(event.messages);
		},
	);
	onCleanup(unsubscribe);

	createEffect(() => {
		if (!props.active) return;
		if (mode() !== "edit") textareaRef = undefined;
	});

	createEffect(() => {
		if (mode() === "edit") return;
		messages();
		const index = selected();
		queueMicrotask(() => {
			scrollRef?.scrollChildIntoView(`queue-editor-row-${index}`);
		});
	});

	function move(delta: number): void {
		const count = messages().length;
		if (count === 0) return;
		setSelected((current) => (current + delta + count) % count);
	}

	function startEdit(): void {
		const message = messages()[selected()];
		if (message === undefined) return;
		setDraft(props.runtime.getPendingMessageDrafts()[selected()] ?? message);
		setMode("edit");
	}

	function saveEdit(): void {
		props.runtime.updatePendingMessage(selected(), draft());
		setMode("list");
		syncMessages();
	}

	function deleteSelected(): void {
		props.runtime.removePendingMessage(selected());
		syncMessages();
	}

	function clearAll(): void {
		props.runtime.clearPendingMessages();
		props.done();
	}

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => props.active && mode() === "list",
			priority: 220,
			commands: [
				{
					name: "queue-editor.close",
					desc: "Close queue editor",
					group: "queue-editor",
					run: () => props.done(),
				},
				{
					name: "queue-editor.move-up",
					desc: "Move selection up",
					group: "queue-editor",
					run: () => move(-1),
				},
				{
					name: "queue-editor.move-down",
					desc: "Move selection down",
					group: "queue-editor",
					run: () => move(1),
				},
				{
					name: "queue-editor.edit",
					desc: "Edit selected message",
					group: "queue-editor",
					run: startEdit,
				},
				{
					name: "queue-editor.delete",
					desc: "Delete selected message",
					group: "queue-editor",
					run: deleteSelected,
				},
				{
					name: "queue-editor.clear",
					desc: "Clear queued messages",
					group: "queue-editor",
					run: () => {
						if (messages().length <= 1) {
							clearAll();
							return;
						}
						setMode("confirm-clear");
					},
				},
			],
			bindings: [
				{
					key: "escape",
					cmd: "queue-editor.close",
					desc: "Close queue editor",
					group: "queue-editor",
				},
				{
					key: "up",
					cmd: "queue-editor.move-up",
					desc: "Move selection up",
					group: "queue-editor",
				},
				{
					key: "down",
					cmd: "queue-editor.move-down",
					desc: "Move selection down",
					group: "queue-editor",
				},
				{
					key: "return",
					cmd: "queue-editor.edit",
					desc: "Edit selected message",
					group: "queue-editor",
				},
				{
					key: "d",
					cmd: "queue-editor.delete",
					desc: "Delete selected message",
					group: "queue-editor",
				},
				{
					key: "c",
					cmd: "queue-editor.clear",
					desc: "Clear queued messages",
					group: "queue-editor",
				},
			],
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => props.active && mode() === "edit",
			priority: 230,
			commands: [
				{
					name: "queue-editor.cancel-edit",
					desc: "Cancel queue message edit",
					group: "queue-editor",
					run: () => setMode("list"),
				},
			],
			bindings: [
				{
					key: "escape",
					cmd: "queue-editor.cancel-edit",
					desc: "Cancel queue message edit",
					group: "queue-editor",
				},
			],
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => props.active && mode() === "confirm-clear",
			priority: 230,
			commands: [
				{
					name: "queue-editor.confirm-clear",
					desc: "Clear queued messages",
					group: "queue-editor",
					run: clearAll,
				},
				{
					name: "queue-editor.cancel-clear",
					desc: "Cancel clear queued messages",
					group: "queue-editor",
					run: () => setMode("list"),
				},
			],
			bindings: [
				{
					key: "return",
					cmd: "queue-editor.confirm-clear",
					desc: "Clear queued messages",
					group: "queue-editor",
				},
				{
					key: "escape",
					cmd: "queue-editor.cancel-clear",
					desc: "Cancel clear queued messages",
					group: "queue-editor",
				},
			],
		}),
	);

	return (
		<Dialog.Root
			surfaceProps={props.surfaceProps}
			width="70%"
			maxWidth={88}
			minWidth={48}
			height="60%"
		>
			<Dialog.Header>
				<Dialog.Title>Queued follow-ups</Dialog.Title>
				<Dialog.Meta>{`${messages().length} pending`}</Dialog.Meta>
			</Dialog.Header>

			<Dialog.Body>
				<Show
					when={mode() === "edit"}
					fallback={
						<scrollbox
							ref={(value) => {
								scrollRef = value as QueueScrollRef | undefined;
							}}
							flexGrow={1}
							scrollY
							style={scrollbarStyle()}
						>
							<box flexDirection="column" gap={0} width="100%">
								<For each={messages()}>
									{(message, index) => {
										const focused = () => index() === selected();
										return (
											<box
												id={`queue-editor-row-${index()}`}
												flexDirection="column"
												paddingX={1}
												backgroundColor={focused() ? theme.bgMuted : undefined}
											>
												<text
													fg={
														focused() ? theme.textPrimary : theme.textSecondary
													}
												>{`${focused() ? CHEVRON_RIGHT : " "} ${index() + 1}  ${previewMessage(message)}`}</text>
												<text
													fg={theme.textMuted}
												>{`    ${messageMeta(message)}`}</text>
											</box>
										);
									}}
								</For>
								<Show when={mode() === "confirm-clear"}>
									<box marginTop={1} paddingX={1}>
										<text fg={theme.warningText}>
											Clear all queued follow-ups?
										</text>
									</box>
								</Show>
							</box>
						</scrollbox>
					}
				>
					<MessageComposer
						ref={(value) => {
							textareaRef = value;
						}}
						initialValue={draft()}
						focused={props.active && mode() === "edit"}
						maxHeight={12}
						borderColor={theme.borderAccent}
						keyBindings={[
							{ name: "return", action: "submit" },
							{ name: "return", shift: true, action: "newline" },
						]}
						onContentChange={() => setDraft(textareaRef?.plainText ?? "")}
						onSubmit={saveEdit}
					/>
				</Show>
			</Dialog.Body>

			<Dialog.Footer>
				<HintBar
					borderless
					bindings={
						mode() === "edit"
							? EDIT_HINTS
							: mode() === "confirm-clear"
								? CONFIRM_HINTS
								: LIST_HINTS
					}
				/>
			</Dialog.Footer>
		</Dialog.Root>
	);
}
