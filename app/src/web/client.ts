import {
	createClientState,
	hydrateMessageReference,
	isRecord,
	ProtocolSyncError,
	prependMessages,
	reduceClientRecord,
	withConnectionPhase,
} from "./client-state.js";

function requiredElement<T extends Element>(selector: string): T {
	const element = document.querySelector(selector);
	if (!element) throw new Error(`Missing required element: ${selector}`);
	return element as T;
}

const connectionStatus = requiredElement<HTMLSpanElement>("#connection-status");
const workspaceLabel = requiredElement<HTMLSpanElement>("#workspace-label");
const modelLabel = requiredElement<HTMLSpanElement>("#model-label");
const sessionLabel = requiredElement<HTMLSpanElement>("#session-label");
const transcript = requiredElement<HTMLElement>("#transcript");
const loadEarlierButton = requiredElement<HTMLButtonElement>("#load-earlier");
const emptyState = requiredElement<HTMLElement>("#empty-state");
const messageList = requiredElement<HTMLDivElement>("#message-list");
const activity = requiredElement<HTMLElement>("#activity");
const activityList = requiredElement<HTMLDivElement>("#activity-list");
const composerForm = requiredElement<HTMLFormElement>("#composer-form");
const composerInput = requiredElement<HTMLTextAreaElement>("#composer-input");
const attachmentButton =
	requiredElement<HTMLButtonElement>("#attachment-button");
const attachmentInput = requiredElement<HTMLInputElement>("#attachment-input");
const attachmentList = requiredElement<HTMLDivElement>("#attachment-list");
const queueStatus = requiredElement<HTMLSpanElement>("#queue-status");
const abortButton = requiredElement<HTMLButtonElement>("#abort-button");
const submitButton = requiredElement<HTMLButtonElement>("#submit-button");
const appStatus = requiredElement<HTMLParagraphElement>("#app-status");
const interactionDialog = requiredElement<HTMLDialogElement>(
	"#interaction-dialog",
);
const interactionForm = requiredElement<HTMLFormElement>("#interaction-form");
const interactionTitle =
	requiredElement<HTMLHeadingElement>("#interaction-title");
const interactionMessage = requiredElement<HTMLParagraphElement>(
	"#interaction-message",
);
const interactionFields = requiredElement<HTMLDivElement>(
	"#interaction-fields",
);
const interactionActions = requiredElement<HTMLElement>("#interaction-actions");

type PendingCommand = {
	resolve(record: Record<string, unknown>): void;
	reject(error: Error): void;
};

type PendingAttachment = {
	file: File;
	id?: string;
};

const MAX_INTERACTION_BYTES = 2 * 1024 * 1024;

let state = createClientState();
let socket: WebSocket | null = null;
let reconnectTimer: number | null = null;
let reconnectAttempt = 0;
let requireSnapshot = false;
let activeSyncMode: string | null = null;
let submitting = false;
let loadingPendingInteractions = false;
let commandCounter = 0;
let currentInteractionId: string | null = null;
let pendingAttachments: PendingAttachment[] = [];
let loadingEarlier = false;
let scrollAnchor: { height: number; top: number } | null = null;
const messageNodes = new Map<string, HTMLElement>();
const renderedMessages = new Map<string, unknown>();
const toolNodes = new Map<string, HTMLDetailsElement>();
const renderedTools = new Map<string, unknown>();
const interactionHydrationErrors = new Map<string, string>();
const hydratingMessages = new Set<string>();
const hydratingInteractions = new Set<string>();
const pendingCommands = new Map<string, PendingCommand>();
const queuedRecords: unknown[] = [];
let protocolTimer: number | null = null;
let domRenderFrame: number | null = null;

function setStatus(message: string, isError = false): void {
	appStatus.textContent = message;
	appStatus.dataset.error = String(isError);
}

function displayValue(value: unknown): string {
	if (typeof value === "string") return value;
	if (value === undefined || value === null) return "";
	try {
		return JSON.stringify(value, null, 2);
	} catch {
		return String(value);
	}
}

function messageParts(message: unknown): Array<Record<string, unknown>> {
	if (!isRecord(message)) return [];
	if (typeof message.content === "string") {
		return [{ type: "text", text: message.content }];
	}
	return Array.isArray(message.content) ? message.content.filter(isRecord) : [];
}

function messageRole(message: unknown): string {
	return isRecord(message) && typeof message.role === "string"
		? message.role
		: "message";
}

function renderMessage(article: HTMLElement, message: unknown): void {
	const role = messageRole(message);
	article.className = "message";
	article.dataset.role = role;
	article.replaceChildren();

	const label = document.createElement("p");
	label.className = "message-label";
	label.textContent =
		role === "assistant" ? "Kit" : role === "user" ? "You" : "Message";
	article.append(label);

	const parts = messageParts(message);
	if (parts.length === 0) {
		const content = document.createElement("div");
		content.className = "message-content";
		content.textContent =
			isRecord(message) && message.type === "message_reference"
				? "Loading message…"
				: isRecord(message) && message.type === "message_unavailable"
					? "This message is too large to display."
					: displayValue(message);
		article.append(content);
		return;
	}
	for (const part of parts) {
		if (part.type === "image" && typeof part.attachmentId === "string") {
			const image = document.createElement("img");
			image.src = `/api/attachments/${encodeURIComponent(part.attachmentId)}`;
			image.alt =
				typeof part.filename === "string" ? part.filename : "Attached image";
			image.loading = "lazy";
			article.append(image);
			continue;
		}
		const content = document.createElement("div");
		content.className =
			part.type === "thinking"
				? "message-content message-thinking"
				: "message-content";
		content.textContent =
			typeof part.text === "string"
				? part.text
				: typeof part.thinking === "string"
					? part.thinking
					: displayValue(part);
		article.append(content);
	}
}

function placeChild(
	container: HTMLElement,
	child: HTMLElement,
	index: number,
): void {
	const current = container.children.item(index);
	if (current !== child) container.insertBefore(child, current);
}

function renderMessages(): void {
	const activeKeys = new Set(state.messageKeys);
	for (const [index, message] of state.messages.entries()) {
		const key = state.messageKeys[index] ?? `message:${index}`;
		let article = messageNodes.get(key);
		if (!article) {
			article = document.createElement("article");
			messageNodes.set(key, article);
		}
		if (renderedMessages.get(key) !== message) {
			renderMessage(article, message);
			renderedMessages.set(key, message);
		}
		placeChild(messageList, article, index);
	}
	for (const [key, article] of messageNodes) {
		if (activeKeys.has(key)) continue;
		article.remove();
		messageNodes.delete(key);
		renderedMessages.delete(key);
	}
	emptyState.hidden = state.messages.length > 0;
	loadEarlierButton.hidden = state.messageOffset === 0;
	loadEarlierButton.disabled = loadingEarlier || state.phase !== "live";
}

function renderActivity(): void {
	activity.hidden = state.tools.length === 0;
	const activeIds = new Set(state.tools.map((tool) => tool.id));
	for (const [index, tool] of state.tools.entries()) {
		let details = toolNodes.get(tool.id);
		if (!details) {
			details = document.createElement("details");
			details.className = "tool-activity";
			const summary = document.createElement("summary");
			const result = document.createElement("pre");
			result.className = "tool-result";
			details.append(summary, result);
			toolNodes.set(tool.id, details);
		}
		if (renderedTools.get(tool.id) !== tool) {
			details.dataset.status = tool.status;
			details.dataset.error = String(tool.isError);
			const summary = details.querySelector("summary");
			const result = details.querySelector("pre");
			if (summary) summary.textContent = tool.name;
			if (result) result.textContent = displayValue(tool.result ?? tool.args);
			renderedTools.set(tool.id, tool);
		}
		placeChild(activityList, details, index);
	}
	for (const [id, details] of toolNodes) {
		if (activeIds.has(id)) continue;
		details.remove();
		toolNodes.delete(id);
		renderedTools.delete(id);
	}
}

function shortSessionId(value: unknown): string {
	return typeof value === "string" ? value.slice(0, 8) : "";
}

function renderHeader(): void {
	connectionStatus.dataset.phase = state.phase;
	const connectionText =
		state.phase === "live"
			? "Connected"
			: state.phase === "synchronizing"
				? "Synchronizing"
				: state.phase === "connecting"
					? "Connecting"
					: "Disconnected";
	if (connectionStatus.textContent !== connectionText) {
		connectionStatus.textContent = connectionText;
	}
	workspaceLabel.textContent =
		typeof state.serverState.cwd === "string" ? state.serverState.cwd : "";
	workspaceLabel.title = workspaceLabel.textContent;
	const model = state.serverState.model;
	modelLabel.textContent = isRecord(model)
		? [model.provider, model.id]
				.filter((value) => typeof value === "string")
				.join("/")
		: "";
	modelLabel.title = modelLabel.textContent;
	sessionLabel.textContent =
		typeof state.serverState.sessionName === "string"
			? state.serverState.sessionName
			: shortSessionId(state.serverState.sessionId);
	sessionLabel.title = sessionLabel.textContent;
}

function isStreaming(): boolean {
	return state.serverState.isStreaming === true;
}

function renderComposer(): void {
	const enabled = state.phase === "live" && !submitting;
	composerInput.disabled = !enabled;
	submitButton.disabled = !enabled;
	attachmentButton.disabled = !enabled || isStreaming();
	attachmentInput.disabled = attachmentButton.disabled;
	abortButton.hidden = !isStreaming();
	abortButton.disabled = !enabled;
	queueStatus.textContent =
		state.queuedMessageCount > 0
			? `${state.queuedMessageCount} queued`
			: isStreaming()
				? "Send queues a follow-up"
				: "";
}

function button(
	label: string,
	value: string,
	variant?: string,
): HTMLButtonElement {
	const element = document.createElement("button");
	element.type = "button";
	element.value = value;
	element.textContent = label;
	if (variant) element.dataset.variant = variant;
	return element;
}

function labeledInput(
	labelText: string,
	name: string,
	value = "",
	placeholder = "",
): HTMLLabelElement {
	const label = document.createElement("label");
	label.className = "interaction-question";
	const text = document.createElement("span");
	text.textContent = labelText;
	const input = document.createElement("input");
	input.name = name;
	input.value = value;
	input.placeholder = placeholder;
	label.append(text, input);
	return label;
}

function interactionResponse(
	request: Record<string, unknown>,
	cancelled: boolean,
): unknown {
	const payload = isRecord(request.payload) ? request.payload : {};
	if (request.kind === "confirm") return { confirmed: !cancelled };
	if (request.kind === "input") {
		const input = interactionFields.querySelector<HTMLInputElement>("input");
		return { value: cancelled ? null : (input?.value ?? "") };
	}
	if (request.kind === "select") {
		const selected = interactionFields.querySelector<HTMLInputElement>(
			'input[name="option"]:checked',
		);
		return { optionId: cancelled ? null : (selected?.value ?? null) };
	}
	if (request.kind === "guided_questions") {
		if (cancelled) return { cancelled: true, answers: {} };
		const answers: Record<string, unknown> = {};
		for (const question of Array.isArray(payload.questions)
			? payload.questions.filter(isRecord)
			: []) {
			if (
				typeof question.id !== "string" ||
				typeof question.kind !== "string"
			) {
				continue;
			}
			if (question.kind === "multiselect") {
				answers[question.id] = Array.from(
					interactionFields.querySelectorAll<HTMLInputElement>(
						`input[name="${CSS.escape(question.id)}"]:checked`,
					),
				).map((input) => input.value);
			} else if (question.kind === "boolean") {
				const selected = interactionFields.querySelector<HTMLInputElement>(
					`input[name="${CSS.escape(question.id)}"]:checked`,
				);
				if (selected) answers[question.id] = selected.value === "true";
			} else {
				const input = interactionFields.querySelector<
					HTMLInputElement | HTMLSelectElement
				>(`[name="${CSS.escape(question.id)}"]`);
				answers[question.id] = input?.value ?? "";
			}
		}
		return { cancelled: false, answers };
	}
	return {};
}

async function answerInteraction(
	request: Record<string, unknown>,
	cancelled: boolean,
): Promise<void> {
	if (typeof request.id !== "string") return;
	for (const element of Array.from(
		interactionActions.querySelectorAll("button"),
	)) {
		element.disabled = true;
	}
	try {
		await sendCommand({
			type: "ui_response",
			requestId: request.id,
			response: interactionResponse(request, cancelled),
		});
	} catch (error) {
		setStatus(error instanceof Error ? error.message : String(error), true);
		for (const element of Array.from(
			interactionActions.querySelectorAll("button"),
		)) {
			element.disabled = false;
		}
	}
}

async function hydrateInteraction(requestId: string): Promise<void> {
	if (hydratingInteractions.has(requestId) || state.phase !== "live") return;
	hydratingInteractions.add(requestId);
	try {
		const chunks: Uint8Array[] = [];
		let offset = 0;
		let complete = false;
		while (!complete) {
			const response = await sendCommand({
				type: "get_pending_interaction_chunk",
				requestId,
				offset,
			});
			if (!isRecord(response.data) || typeof response.data.data !== "string") {
				throw new Error("Invalid interaction chunk response");
			}
			const totalBytes = response.data.totalBytes;
			if (
				typeof totalBytes !== "number" ||
				!Number.isSafeInteger(totalBytes) ||
				totalBytes < 0 ||
				totalBytes > MAX_INTERACTION_BYTES ||
				response.data.offset !== offset
			) {
				throw new Error(
					"Interaction payload exceeds the client recovery limit",
				);
			}
			const binary = atob(response.data.data);
			const bytes = Uint8Array.from(binary, (character) =>
				character.charCodeAt(0),
			);
			chunks.push(bytes);
			const nextOffset = response.data.nextOffset;
			if (
				typeof nextOffset !== "number" ||
				!Number.isSafeInteger(nextOffset) ||
				nextOffset !== offset + bytes.length ||
				nextOffset > totalBytes ||
				(nextOffset === offset && response.data.complete !== true)
			) {
				throw new Error("Interaction chunks are not contiguous");
			}
			offset = nextOffset;
			complete = response.data.complete === true;
			if (complete && offset !== totalBytes) {
				throw new Error("Interaction payload ended at the wrong offset");
			}
		}
		const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
		const joined = new Uint8Array(total);
		let cursor = 0;
		for (const chunk of chunks) {
			joined.set(chunk, cursor);
			cursor += chunk.length;
		}
		const hydrated: unknown = JSON.parse(new TextDecoder().decode(joined));
		if (!isRecord(hydrated) || hydrated.id !== requestId) {
			throw new Error("Interaction recovery returned the wrong request");
		}
		state = {
			...state,
			pendingInteractions: state.pendingInteractions.map((request) =>
				isRecord(request) && request.id === requestId ? hydrated : request,
			),
		};
		interactionHydrationErrors.delete(requestId);
		currentInteractionId = null;
	} catch (error) {
		const message = error instanceof Error ? error.message : String(error);
		interactionHydrationErrors.set(requestId, message);
		setStatus(message, true);
		currentInteractionId = null;
	} finally {
		hydratingInteractions.delete(requestId);
		render();
	}
}

function renderGuidedQuestions(payload: Record<string, unknown>): void {
	const questions = Array.isArray(payload.questions)
		? payload.questions.filter(isRecord)
		: [];
	for (const question of questions) {
		if (
			typeof question.id !== "string" ||
			typeof question.label !== "string" ||
			typeof question.kind !== "string"
		) {
			continue;
		}
		const field = document.createElement("fieldset");
		field.dataset.questionKind = question.kind;
		field.dataset.required = String(question.required === true);
		const legend = document.createElement("legend");
		legend.textContent = question.label;
		field.append(legend);
		if (typeof question.help === "string") {
			const help = document.createElement("small");
			help.textContent = question.help;
			field.append(help);
		}
		if (question.kind === "text") {
			const input = document.createElement("input");
			input.name = question.id;
			input.required = question.required === true;
			field.append(input);
		} else {
			const options =
				question.kind === "boolean"
					? ["true", "false"]
					: Array.isArray(question.options)
						? question.options.filter(
								(option): option is string => typeof option === "string",
							)
						: [];
			for (const option of options) {
				const label = document.createElement("label");
				label.className = "interaction-option";
				const input = document.createElement("input");
				input.type = question.kind === "multiselect" ? "checkbox" : "radio";
				input.name = question.id;
				input.value = option;
				input.required =
					question.required === true && question.kind !== "multiselect";
				label.append(input, document.createTextNode(option));
				field.append(label);
			}
		}
		interactionFields.append(field);
	}
}

function setInteractionControlsDisabled(disabled: boolean): void {
	for (const control of Array.from(
		interactionDialog.querySelectorAll<
			HTMLInputElement | HTMLButtonElement | HTMLSelectElement
		>("input, button, select"),
	)) {
		control.disabled = disabled;
	}
}

function renderInteraction(): void {
	const request = state.pendingInteractions.find(isRecord);
	if (!request) {
		currentInteractionId = null;
		if (interactionDialog.open) interactionDialog.close();
		return;
	}
	if (typeof request.id !== "string") return;
	if (state.phase !== "live") {
		if (currentInteractionId === request.id)
			setInteractionControlsDisabled(true);
		return;
	}
	if (currentInteractionId === request.id) {
		setInteractionControlsDisabled(false);
		return;
	}
	currentInteractionId = request.id;
	if (request.payloadOmitted === true) {
		const hydrationError = interactionHydrationErrors.get(request.id);
		interactionTitle.textContent = "Kit needs your input";
		interactionMessage.textContent = hydrationError ?? "Loading interaction…";
		interactionFields.replaceChildren();
		interactionActions.replaceChildren();
		const cancel = button("Cancel", "cancel", "ghost");
		cancel.addEventListener(
			"click",
			() => void answerInteraction(request, true),
		);
		interactionActions.append(cancel);
		if (hydrationError) {
			const retry = button("Retry", "retry", "primary");
			retry.addEventListener("click", () => {
				interactionHydrationErrors.delete(request.id as string);
				currentInteractionId = null;
				renderInteraction();
			});
			interactionActions.append(retry);
		} else {
			void hydrateInteraction(request.id);
		}
		if (!interactionDialog.open) interactionDialog.showModal();
		return;
	}
	const payload = isRecord(request.payload) ? request.payload : {};
	interactionTitle.textContent =
		typeof payload.title === "string" ? payload.title : "Kit needs your input";
	interactionMessage.textContent =
		typeof payload.message === "string" ? payload.message : "";
	interactionFields.replaceChildren();
	interactionActions.replaceChildren();

	if (request.kind === "input") {
		interactionFields.append(
			labeledInput(
				"Response",
				"value",
				typeof payload.initialValue === "string" ? payload.initialValue : "",
				typeof payload.placeholder === "string" ? payload.placeholder : "",
			),
		);
	} else if (request.kind === "select") {
		for (const option of Array.isArray(payload.options)
			? payload.options.filter(isRecord)
			: []) {
			if (typeof option.id !== "string" || typeof option.label !== "string") {
				continue;
			}
			const label = document.createElement("label");
			label.className = "interaction-option";
			const input = document.createElement("input");
			input.type = "radio";
			input.name = "option";
			input.value = option.id;
			input.required = true;
			const text = document.createElement("span");
			text.textContent = option.label;
			label.append(input, text);
			if (typeof option.description === "string") {
				const description = document.createElement("small");
				description.textContent = option.description;
				label.append(description);
			}
			interactionFields.append(label);
		}
	} else if (request.kind === "guided_questions") {
		renderGuidedQuestions(payload);
	}

	const cancel = button(
		typeof payload.cancelLabel === "string" ? payload.cancelLabel : "Cancel",
		"cancel",
		"ghost",
	);
	cancel.addEventListener("click", () => void answerInteraction(request, true));
	const confirm = button(
		typeof payload.confirmLabel === "string"
			? payload.confirmLabel
			: request.kind === "confirm"
				? "Confirm"
				: "Submit",
		"confirm",
		"primary",
	);
	confirm.type = "submit";
	interactionActions.append(cancel, confirm);
	if (!interactionDialog.open) interactionDialog.showModal();
}

function renderAttachments(): void {
	attachmentList.replaceChildren();
	for (const attachment of pendingAttachments) {
		const chip = document.createElement("span");
		chip.className = "attachment-chip";
		chip.textContent = attachment.file.name;
		const remove = button("Remove", attachment.file.name, "ghost");
		remove.dataset.size = "small";
		remove.disabled = submitting;
		remove.addEventListener("click", () => void removeAttachment(attachment));
		chip.append(remove);
		attachmentList.append(chip);
	}
}

function render(): void {
	const nearBottom =
		transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight <
		96;
	renderHeader();
	renderMessages();
	renderActivity();
	renderComposer();
	renderInteraction();
	if (scrollAnchor) {
		transcript.scrollTop =
			scrollAnchor.top + (transcript.scrollHeight - scrollAnchor.height);
		scrollAnchor = null;
	} else if (nearBottom) {
		transcript.scrollTop = transcript.scrollHeight;
	}
	if (state.lastError) setStatus(state.lastError, true);
}

function rejectPendingCommands(error: Error): void {
	for (const pending of pendingCommands.values()) pending.reject(error);
	pendingCommands.clear();
}

function reconnectUrl(): string {
	const url = new URL("/api/rpc", window.location.href);
	url.protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
	if (!requireSnapshot && state.streamId) {
		url.searchParams.set("streamId", state.streamId);
		url.searchParams.set("after", String(state.sequence));
	}
	return url.href;
}

function scheduleReconnect(): void {
	if (reconnectTimer !== null) return;
	const delay = Math.min(500 * 2 ** reconnectAttempt, 10_000);
	reconnectAttempt += 1;
	reconnectTimer = window.setTimeout(() => {
		reconnectTimer = null;
		connect();
	}, delay);
}

function scheduleRender(): void {
	if (domRenderFrame !== null) return;
	domRenderFrame = requestAnimationFrame(() => {
		domRenderFrame = null;
		render();
	});
}

function messageDelta(record: unknown): Record<string, unknown> | null {
	if (
		!isRecord(record) ||
		record.type !== "message_update" ||
		!isRecord(record.assistantMessageEvent)
	) {
		return null;
	}
	const event = record.assistantMessageEvent;
	return (event.type === "text_delta" || event.type === "thinking_delta") &&
		typeof event.delta === "string" &&
		typeof event.contentIndex === "number"
		? event
		: null;
}

function flushProtocolRecords(): void {
	if (protocolTimer !== null) clearTimeout(protocolTimer);
	protocolTimer = null;
	try {
		const records = queuedRecords.splice(0);
		for (let index = 0; index < records.length; index += 1) {
			let record = records[index];
			const delta = messageDelta(record);
			if (delta && isRecord(record) && typeof record.sequence === "number") {
				let combinedDelta = delta.delta as string;
				let lastSequence = record.sequence;
				while (index + 1 < records.length) {
					const next = records[index + 1];
					const nextDelta = messageDelta(next);
					if (
						!isRecord(next) ||
						!nextDelta ||
						next.streamId !== record.streamId ||
						next.sequence !== lastSequence + 1 ||
						nextDelta.type !== delta.type ||
						nextDelta.contentIndex !== delta.contentIndex
					) {
						break;
					}
					combinedDelta += nextDelta.delta as string;
					lastSequence = next.sequence as number;
					index += 1;
				}
				if (lastSequence !== record.sequence) {
					record = {
						...record,
						assistantMessageEvent: { ...delta, delta: combinedDelta },
					};
					state = reduceClientRecord(state, record);
					state = { ...state, sequence: lastSequence };
					continue;
				}
			}
			if (isRecord(record) && record.type === "sync") {
				activeSyncMode = typeof record.mode === "string" ? record.mode : null;
			}
			if (isRecord(record) && record.type === "resync_required") {
				throw new ProtocolSyncError("The session requires a fresh snapshot");
			}
			state = reduceClientRecord(state, record);
			if (isRecord(record) && record.type === "sync_complete") {
				if (activeSyncMode === "snapshot") requireSnapshot = false;
				activeSyncMode = null;
			}
		}
		if (state.phase === "live") {
			reconnectAttempt = 0;
			void loadPendingInteractions();
			void hydrateVisibleMessageReferences();
		}
		scheduleRender();
	} catch (error) {
		requireSnapshot = true;
		setStatus(error instanceof Error ? error.message : String(error), true);
		socket?.close();
	}
}

function enqueueProtocolRecord(record: unknown): void {
	queuedRecords.push(record);
	if (queuedRecords.length >= 256) {
		flushProtocolRecords();
		return;
	}
	if (protocolTimer === null) {
		protocolTimer = window.setTimeout(flushProtocolRecords, 0);
	}
}

function connect(): void {
	state = withConnectionPhase(state, "connecting");
	render();
	const nextSocket = new WebSocket(reconnectUrl());
	socket = nextSocket;
	nextSocket.addEventListener("message", (event) => {
		if (socket !== nextSocket) return;
		try {
			const record: unknown = JSON.parse(String(event.data));
			if (isRecord(record) && record.type === "response") {
				if (queuedRecords.length > 0) flushProtocolRecords();
				const id = record.id;
				if (typeof id === "string") {
					const pending = pendingCommands.get(id);
					if (pending) {
						pendingCommands.delete(id);
						if (record.success === true) pending.resolve(record);
						else {
							pending.reject(
								new Error(
									typeof record.error === "string"
										? record.error
										: "Command failed",
								),
							);
						}
					}
				}
				return;
			}
			enqueueProtocolRecord(record);
		} catch (error) {
			requireSnapshot = true;
			setStatus(error instanceof Error ? error.message : String(error), true);
			nextSocket.close();
		}
	});
	nextSocket.addEventListener("close", () => {
		if (socket !== nextSocket) return;
		socket = null;
		queuedRecords.length = 0;
		if (protocolTimer !== null) clearTimeout(protocolTimer);
		protocolTimer = null;
		state = withConnectionPhase(state, "disconnected");
		rejectPendingCommands(
			new Error("Connection closed before a response arrived"),
		);
		render();
		scheduleReconnect();
	});
	nextSocket.addEventListener("error", () => {
		setStatus("Unable to connect to the Kit session.", true);
	});
}

function sendCommand(
	command: Record<string, unknown>,
): Promise<Record<string, unknown>> {
	if (
		!socket ||
		socket.readyState !== WebSocket.OPEN ||
		state.phase !== "live"
	) {
		return Promise.reject(new Error("Kit is not connected"));
	}
	commandCounter += 1;
	const id = `web-${Date.now().toString(36)}-${commandCounter.toString(36)}`;
	return new Promise((resolve, reject) => {
		pendingCommands.set(id, { resolve, reject });
		socket?.send(JSON.stringify({ ...command, id }));
	});
}

async function uploadAttachment(
	attachment: PendingAttachment,
): Promise<string> {
	if (attachment.id) return attachment.id;
	const form = new FormData();
	form.append("file", attachment.file);
	const response = await fetch("/api/attachments", {
		method: "POST",
		body: form,
	});
	const payload: unknown = await response.json();
	if (!response.ok || !isRecord(payload) || !isRecord(payload.attachment)) {
		throw new Error(
			isRecord(payload) && typeof payload.error === "string"
				? payload.error
				: `Attachment upload failed (${response.status})`,
		);
	}
	if (typeof payload.attachment.id !== "string") {
		throw new Error("Attachment upload returned no id");
	}
	attachment.id = payload.attachment.id;
	return attachment.id;
}

async function removeAttachment(attachment: PendingAttachment): Promise<void> {
	pendingAttachments = pendingAttachments.filter((item) => item !== attachment);
	renderAttachments();
	setStatus(
		pendingAttachments.length === 0
			? "Attachment removed"
			: `${pendingAttachments.length} attachments selected`,
	);
	if (attachment.id) {
		try {
			const response = await fetch(
				`/api/attachments/${encodeURIComponent(attachment.id)}`,
				{ method: "DELETE" },
			);
			if (!response.ok && response.status !== 404) {
				throw new Error(`Attachment removal failed (${response.status})`);
			}
		} catch (error) {
			setStatus(error instanceof Error ? error.message : String(error), true);
		}
	}
}

async function resolveMessageReference(message: unknown): Promise<unknown> {
	if (
		!isRecord(message) ||
		message.type !== "message_reference" ||
		typeof message.token !== "string"
	) {
		return message;
	}
	const chunks: Uint8Array[] = [];
	let offset = 0;
	let complete = false;
	while (!complete) {
		const response = await sendCommand({
			type: "get_message_chunk",
			token: message.token,
			offset,
		});
		if (!isRecord(response.data) || typeof response.data.data !== "string") {
			throw new Error("Invalid message chunk response");
		}
		const binary = atob(response.data.data);
		const bytes = Uint8Array.from(binary, (character) =>
			character.charCodeAt(0),
		);
		chunks.push(bytes);
		offset =
			typeof response.data.nextOffset === "number"
				? response.data.nextOffset
				: offset + bytes.length;
		complete = response.data.complete === true;
	}
	const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
	const joined = new Uint8Array(total);
	let cursor = 0;
	for (const chunk of chunks) {
		joined.set(chunk, cursor);
		cursor += chunk.length;
	}
	return JSON.parse(new TextDecoder().decode(joined));
}

type RecoveredMessage = {
	message: unknown;
	rebased: boolean;
};

async function recoverMessageReference(
	message: Record<string, unknown>,
	messageIndex: number,
): Promise<RecoveredMessage> {
	let candidate: unknown = message;
	let lastError: unknown = null;
	let rebased = false;
	for (let attempt = 0; attempt < 3; attempt += 1) {
		try {
			return {
				message: await resolveMessageReference(candidate),
				rebased,
			};
		} catch (error) {
			lastError = error;
			if (state.phase !== "live") throw error;
			const response = await sendCommand({
				type: "get_messages",
				offset: messageIndex,
				limit: 1,
			});
			if (
				!isRecord(response.data) ||
				response.data.offset !== messageIndex ||
				!Array.isArray(response.data.messages) ||
				response.data.messages.length !== 1
			) {
				throw new Error("Message recovery returned the wrong record");
			}
			candidate = response.data.messages[0];
			rebased = true;
			if (!isRecord(candidate) || candidate.type !== "message_reference") {
				return { message: candidate, rebased };
			}
		}
	}
	return {
		message: {
			type: "message_unavailable",
			role: message.role,
			messageIndex,
			reason:
				lastError instanceof Error ? lastError.message : "recovery_failed",
		},
		rebased: true,
	};
}

async function hydrateVisibleMessageReferences(): Promise<void> {
	for (const [index, message] of state.messages.entries()) {
		if (
			!isRecord(message) ||
			message.type !== "message_reference" ||
			typeof message.token !== "string" ||
			hydratingMessages.has(message.token)
		) {
			continue;
		}
		hydratingMessages.add(message.token);
		try {
			const messageIndex =
				typeof message.messageIndex === "number"
					? message.messageIndex
					: state.messageOffset + index;
			const recovered = await recoverMessageReference(message, messageIndex);
			const currentIndex = state.messages.indexOf(message);
			if (currentIndex < 0) continue;
			state = hydrateMessageReference(
				state,
				currentIndex,
				message,
				recovered.message,
				!recovered.rebased,
			);
			const key = state.messageKeys[currentIndex];
			if (key) renderedMessages.delete(key);
			scheduleRender();
		} catch (error) {
			setStatus(error instanceof Error ? error.message : String(error), true);
		} finally {
			hydratingMessages.delete(message.token);
		}
	}
}

async function loadPendingInteractions(): Promise<void> {
	if (
		loadingPendingInteractions ||
		state.phase !== "live" ||
		state.pendingInteractions.length >= state.totalPendingInteractionCount
	) {
		return;
	}
	loadingPendingInteractions = true;
	const revision = state.interactionRevision;
	try {
		let offset = 0;
		let total = state.totalPendingInteractionCount;
		const requests: unknown[] = [];
		while (offset < total) {
			const response = await sendCommand({
				type: "get_pending_interactions",
				offset,
				limit: Math.min(20, total - offset),
			});
			if (state.interactionRevision !== revision) return;
			if (!isRecord(response.data) || !Array.isArray(response.data.requests)) {
				throw new Error("Invalid pending interaction page response");
			}
			if (
				typeof response.data.offset === "number" &&
				response.data.offset !== offset
			) {
				throw new Error("Pending interaction page is not contiguous");
			}
			if (response.data.requests.length === 0) break;
			for (const request of response.data.requests) {
				if (
					!isRecord(request) ||
					typeof request.id !== "string" ||
					requests.some(
						(existing) => isRecord(existing) && existing.id === request.id,
					)
				) {
					continue;
				}
				requests.push(request);
			}
			offset += response.data.requests.length;
			total =
				typeof response.data.totalRequestCount === "number"
					? response.data.totalRequestCount
					: total;
		}
		if (state.interactionRevision !== revision) return;
		state = {
			...state,
			pendingInteractions: requests,
			pendingInteractionOffset: 0,
			totalPendingInteractionCount: total,
		};
		scheduleRender();
	} catch (error) {
		setStatus(error instanceof Error ? error.message : String(error), true);
	} finally {
		loadingPendingInteractions = false;
		if (
			state.phase === "live" &&
			state.interactionRevision !== revision &&
			state.pendingInteractions.length < state.totalPendingInteractionCount
		) {
			void loadPendingInteractions();
		}
	}
}

async function loadEarlierMessages(): Promise<void> {
	if (loadingEarlier || state.messageOffset === 0) return;
	loadingEarlier = true;
	render();
	try {
		const oldOffset = state.messageOffset;
		const targetOffset = Math.max(0, oldOffset - 50);
		const messages: unknown[] = [];
		let cursor = targetOffset;
		let totalMessageCount = state.totalMessageCount;
		while (cursor < oldOffset) {
			const response = await sendCommand({
				type: "get_messages",
				offset: cursor,
				limit: Math.min(50, oldOffset - cursor),
			});
			if (!isRecord(response.data) || !Array.isArray(response.data.messages)) {
				throw new Error("Invalid message page response");
			}
			if (
				typeof response.data.offset === "number" &&
				response.data.offset !== cursor
			) {
				throw new Error("Message page is not contiguous");
			}
			if (response.data.messages.length === 0) {
				throw new Error("Message history ended before the requested cursor");
			}
			messages.push(
				...(await Promise.all(
					response.data.messages.map(resolveMessageReference),
				)),
			);
			cursor += response.data.messages.length;
			totalMessageCount =
				typeof response.data.totalMessageCount === "number"
					? response.data.totalMessageCount
					: totalMessageCount;
		}
		scrollAnchor = {
			height: transcript.scrollHeight,
			top: transcript.scrollTop,
		};
		state = prependMessages(state, messages, targetOffset, totalMessageCount);
		setStatus("");
	} catch (error) {
		setStatus(error instanceof Error ? error.message : String(error), true);
	} finally {
		loadingEarlier = false;
		render();
	}
}

composerForm.addEventListener("submit", (event) => {
	event.preventDefault();
	void (async () => {
		if (submitting) return;
		const message = composerInput.value.trim();
		if (!message && pendingAttachments.length === 0) return;
		const submittedAttachments = [...pendingAttachments];
		submitting = true;
		render();
		setStatus(
			submittedAttachments.length > 0 ? "Uploading attachments…" : "Sending…",
		);
		try {
			const attachmentIds: string[] = [];
			for (const attachment of submittedAttachments) {
				attachmentIds.push(await uploadAttachment(attachment));
			}
			await sendCommand({
				type: "prompt",
				message,
				...(attachmentIds.length > 0 ? { attachmentIds } : {}),
				...(isStreaming() ? { streamingBehavior: "followUp" } : {}),
			});
			composerInput.value = "";
			pendingAttachments = pendingAttachments.filter(
				(attachment) => !submittedAttachments.includes(attachment),
			);
			setStatus("");
		} catch (error) {
			setStatus(error instanceof Error ? error.message : String(error), true);
		} finally {
			submitting = false;
			renderAttachments();
			render();
		}
	})();
});

composerInput.addEventListener("keydown", (event) => {
	if (
		!submitting &&
		event.key === "Enter" &&
		!event.shiftKey &&
		!event.isComposing
	) {
		event.preventDefault();
		composerForm.requestSubmit();
	}
});

attachmentButton.addEventListener("click", () => attachmentInput.click());

attachmentInput.addEventListener("change", () => {
	if (submitting) return;
	for (const file of Array.from(attachmentInput.files ?? [])) {
		pendingAttachments.push({ file });
	}
	attachmentInput.value = "";
	renderAttachments();
	setStatus(`${pendingAttachments.length} attachments selected`);
});

abortButton.addEventListener("click", () => {
	void sendCommand({ type: "abort" }).catch((error) => {
		setStatus(error instanceof Error ? error.message : String(error), true);
	});
});

loadEarlierButton.addEventListener("click", () => {
	void loadEarlierMessages();
});

interactionDialog.addEventListener("cancel", (event) => {
	event.preventDefault();
	const request = state.pendingInteractions.find(isRecord);
	if (request) void answerInteraction(request, true);
});

interactionForm.addEventListener("submit", (event) => {
	event.preventDefault();
	const request = state.pendingInteractions.find(isRecord);
	if (!request || state.phase !== "live") return;
	for (const field of Array.from(
		interactionFields.querySelectorAll<HTMLFieldSetElement>("fieldset"),
	)) {
		if (
			field.dataset.questionKind !== "multiselect" ||
			field.dataset.required !== "true"
		) {
			continue;
		}
		const inputs = Array.from(
			field.querySelectorAll<HTMLInputElement>('input[type="checkbox"]'),
		);
		const first = inputs[0];
		if (!first) continue;
		first.setCustomValidity(
			inputs.some((input) => input.checked) ? "" : "Select at least one option",
		);
	}
	if (!interactionForm.reportValidity()) return;
	void answerInteraction(request, false);
});

interactionFields.addEventListener("change", (event) => {
	if (event.target instanceof HTMLInputElement) {
		for (const input of Array.from(
			event.target.form?.querySelectorAll<HTMLInputElement>(
				`input[name="${CSS.escape(event.target.name)}"]`,
			) ?? [],
		)) {
			input.setCustomValidity("");
		}
	}
});

window.addEventListener("online", () => {
	if (!socket && reconnectTimer === null) connect();
});

renderAttachments();
render();
connect();
