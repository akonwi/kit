// Kit RPC bridge — browser UI. Browser-native APIs only, no framework.
// All protocol traffic is newline-delimited JSON. The bridge may interleave
// `bridge.*` envelopes of its own; the renderer treats anything whose
// `type` does not start with `bridge.` as Kit RPC traffic.

const state = {
	ws: null,
	wsState: "disconnected", // disconnected | connecting | connected
	connection: "disconnected",
	agent: "offline", // offline | starting | idle | busy | exited
	childPid: null,
	childExit: null,
	runActive: false,
	runId: null,
	runStartedAt: 0,
	nextId: 1,
	currentMessage: null, // { id, content, toolCalls: Map, messageId }
	messages: [], // history of rendered messages (text content for the transcript)
	toolCalls: new Map(), // callId -> tool call DOM
	rawLog: [],
	rawLogMax: 1000,
	transcript: null,
	messageList: null,
	emptyState: null,
	promptInput: null,
	composerForm: null,
	sendButton: null,
	abortButton: null,
	reconnectButton: null,
	statusConnection: null,
	statusAgent: null,
	statusPid: null,
	composerMeta: null,
	protocolLog: null,
	protocolLogBody: null,
	protocolLogToggle: null,
	protocolLogChevron: null,
	protocolLogCount: null,
	protocolLogEntries: null,
	protocolLogClear: null,
	protocolLogAutoscroll: null,
	pendingResponseTimers: new Map(),
};

function el(id) {
	const node = document.getElementById(id);
	if (!node) throw new Error(`missing element #${id}`);
	return node;
}

function setConnection(value) {
	state.connection = value;
	state.statusConnection.textContent = value;
	state.statusConnection.dataset.state = value;
	state.reconnectButton.hidden =
		value === "connecting" || value === "connected";
}

function setAgent(value) {
	state.agent = value;
	state.statusAgent.textContent = value;
	state.statusAgent.dataset.state = value;
	updateSendAvailability();
}

function setChildPid(value) {
	state.childPid = value;
	state.statusPid.textContent = value === null ? "—" : String(value);
}

function updateSendAvailability() {
	const wsOk = state.wsState === "connected";
	const runActive = state.runActive;
	const input = state.promptInput;
	input.disabled = !wsOk;
	state.sendButton.disabled = !wsOk || runActive;
	state.abortButton.hidden = !(wsOk && runActive);
}

function ensureTranscriptVisible() {
	if (!state.transcript) return;
	const el = state.transcript;
	const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
	if (nearBottom) {
		el.scrollTop = el.scrollHeight;
	}
}

function appendMessage(role, content) {
	hideEmptyState();
	const li = document.createElement("li");
	li.className = `message ${role}`;
	const roleSpan = document.createElement("span");
	roleSpan.className = `message-role ${role}`;
	roleSpan.textContent =
		role === "user"
			? "you"
			: role === "assistant"
				? "kit"
				: role === "system"
					? "sys"
					: role;
	const contentSpan = document.createElement("span");
	contentSpan.className = "message-content";
	contentSpan.textContent = content;
	li.append(roleSpan, contentSpan);
	state.messageList.append(li);
	state.messages.push({ role, content, node: li });
	ensureTranscriptVisible();
}

function appendLifecycle(text, kind = "info", tag = null) {
	hideEmptyState();
	const li = document.createElement("li");
	li.className = "message lifecycle";
	const tagSpan = document.createElement("span");
	tagSpan.className = kind === "error" ? "lifecycle-error" : "lifecycle-tag";
	tagSpan.textContent = tag ?? "·";
	const txt = document.createElement("span");
	txt.textContent = text;
	li.append(tagSpan, txt);
	state.messageList.append(li);
	ensureTranscriptVisible();
}

function appendError(text) {
	hideEmptyState();
	const li = document.createElement("li");
	li.className = "message error";
	li.textContent = text;
	state.messageList.append(li);
	ensureTranscriptVisible();
}

function hideEmptyState() {
	if (state.emptyState) state.emptyState.style.display = "none";
}

function startAssistantMessage(messageId) {
	hideEmptyState();
	const li = document.createElement("li");
	li.className = "message assistant";
	li.dataset.messageId = messageId ?? "";
	const roleSpan = document.createElement("span");
	roleSpan.className = "message-role assistant";
	roleSpan.textContent = "kit";
	const contentSpan = document.createElement("span");
	contentSpan.className = "message-content";
	contentSpan.textContent = "";
	li.append(roleSpan, contentSpan);
	state.messageList.append(li);
	state.currentMessage = {
		id: messageId ?? null,
		li,
		contentSpan,
		text: "",
		toolCalls: new Map(),
	};
	ensureTranscriptVisible();
}

function appendAssistantDelta(delta) {
	if (!state.currentMessage) startAssistantMessage(null);
	state.currentMessage.text += delta;
	state.currentMessage.contentSpan.textContent = state.currentMessage.text;
	ensureTranscriptVisible();
}

function finalizeAssistantMessage() {
	if (!state.currentMessage) return;
	state.currentMessage.contentSpan.textContent = state.currentMessage.text;
	state.currentMessage = null;
}

function renderToolCallStart(call) {
	if (!state.currentMessage) startAssistantMessage(null);
	const callId = call.toolCallId;
	if (!callId) return;
	const tool = document.createElement("div");
	tool.className = "tool-call";
	tool.dataset.state = "running";
	tool.dataset.callId = callId;
	const header = document.createElement("div");
	header.className = "tool-call-header";
	const name = document.createElement("span");
	name.className = "tool-call-name";
	name.textContent = call.toolName ?? "tool";
	const id = document.createElement("span");
	id.className = "tool-call-id";
	id.textContent = callId.slice(0, 8);
	header.append(name, id);
	const body = document.createElement("div");
	body.className = "tool-call-body collapsed";
	body.textContent = renderToolArgs(call.args);
	tool.append(header, body);
	state.currentMessage.li.after(tool);
	state.currentMessage.toolCalls.set(callId, tool);
	state.toolCalls.set(callId, tool);
	ensureTranscriptVisible();
}

function renderToolCallUpdate(call) {
	const tool = state.toolCalls.get(call.toolCallId);
	if (!tool) return;
	const body = tool.querySelector(".tool-call-body");
	if (!body) return;
	const result = call.partialResult;
	if (typeof result === "string") {
		body.textContent = result;
	} else if (result !== undefined) {
		body.textContent = JSON.stringify(result, null, 2);
	}
}

function renderToolCallEnd(call) {
	const tool = state.toolCalls.get(call.toolCallId);
	if (!tool) return;
	tool.dataset.state = call.isError ? "error" : "ok";
	const name = tool.querySelector(".tool-call-name");
	if (name) {
		name.dataset.state = call.isError ? "error" : "ok";
		name.textContent = `${call.toolName}${call.isError ? " (failed)" : ""}`;
	}
	const body = tool.querySelector(".tool-call-body");
	if (body) {
		const result = call.result;
		if (typeof result === "string") {
			body.textContent = result;
		} else if (result !== undefined) {
			body.textContent = JSON.stringify(result, null, 2);
		} else {
			body.textContent = "(no result)";
		}
		body.classList.remove("collapsed");
	}
}

function renderToolArgs(args) {
	if (args === undefined || args === null) return "(no args)";
	if (typeof args === "string") return args;
	try {
		return JSON.stringify(args, null, 2);
	} catch {
		return String(args);
	}
}

function appendProtocolLog(direction, record) {
	const ts = new Date();
	const entry = { ts, direction, record };
	state.rawLog.push(entry);
	if (state.rawLog.length > state.rawLogMax) {
		state.rawLog.splice(0, state.rawLog.length - state.rawLogMax);
	}
	renderProtocolLogEntry(entry);
	state.protocolLogCount.textContent = String(state.rawLog.length);
}

function renderProtocolLogEntry(entry) {
	const li = document.createElement("li");
	li.className = "protocol-entry";
	const time = document.createElement("span");
	time.className = "protocol-entry-time";
	time.textContent = entry.ts.toTimeString().slice(0, 8);
	const dir = document.createElement("span");
	dir.className = `protocol-entry-dir ${entry.direction}`;
	if (entry.direction === "tx") dir.textContent = "→";
	else if (entry.direction === "rx") dir.textContent = "←";
	else dir.textContent = "·";
	const type = document.createElement("span");
	type.className = "protocol-entry-type";
	type.textContent =
		typeof entry.record?.type === "string" ? entry.record.type : "?";
	const payload = document.createElement("span");
	payload.className = "protocol-entry-payload";
	try {
		payload.textContent = JSON.stringify(entry.record);
	} catch {
		payload.textContent = String(entry.record);
	}
	li.append(time, dir, type, payload);
	state.protocolLogEntries.append(li);
	if (state.protocolLogAutoscroll.checked) {
		state.protocolLogEntries.scrollTop = state.protocolLogEntries.scrollHeight;
	}
	while (state.protocolLogEntries.children.length > state.rawLogMax) {
		state.protocolLogEntries.firstElementChild?.remove();
	}
}

function clearProtocolLog() {
	state.rawLog.length = 0;
	state.protocolLogEntries.replaceChildren();
	state.protocolLogCount.textContent = "0";
}

function setRunActive(active, reason = "") {
	state.runActive = active;
	state.composerMeta.textContent = active
		? `streaming${reason ? ` · ${reason}` : ""}`
		: state.wsState === "connected"
			? "idle"
			: "disconnected";
	updateSendAvailability();
}

function sendRecord(record) {
	if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
		appendError("not connected — cannot send command");
		return false;
	}
	const line = JSON.stringify(record);
	state.ws.send(line);
	appendProtocolLog("tx", record);
	return true;
}

function nextId(prefix = "ui") {
	state.nextId += 1;
	return `${prefix}-${Date.now().toString(36)}-${state.nextId.toString(36)}`;
}

function handleEvent(record) {
	if (state.protocolLogBody.parentElement?.dataset.open === "true") {
		// log already streamed in via appendProtocolLog
	}
	switch (record.type) {
		case "bridge.client_connected":
			return;
		case "bridge.child_exit": {
			const intentional = record.intentional;
			const code = record.code;
			const duration = record.durationMs;
			state.childPid = null;
			setChildPid(null);
			state.childExit = record;
			if (intentional) {
				setAgent("offline");
			} else {
				setAgent("exited");
				appendError(
					`agent exited (code=${code}) after ${Math.round(duration / 1000)}s`,
				);
			}
			state.runActive = false;
			state.currentMessage = null;
			updateSendAvailability();
			return;
		}
		case "bridge.child_stderr":
			return; // already logged
		case "bridge.child_stdout_parse_error":
			appendError(
				`child emitted invalid JSON: ${record.line?.slice(0, 120) ?? ""}`,
			);
			return;
		case "bridge.error":
			appendError(`bridge error: ${record.error ?? "unknown"}`);
			return;
		case "bridge.pong":
			return;
		case "response": {
			handleResponse(record);
			return;
		}
		case "agent_start":
			appendLifecycle("agent started", "info", "agent");
			return;
		case "turn_start":
			appendLifecycle("turn started", "info", "turn");
			return;
		case "message_start": {
			const message = record.message;
			if (!message) return;
			if (message.role === "user") {
				appendMessage("user", extractUserText(message));
			} else if (message.role === "assistant") {
				startAssistantMessage(message.id ?? null);
			}
			return;
		}
		case "message_update": {
			const ev = record.assistantMessageEvent;
			if (ev?.type === "text_delta" && typeof ev.delta === "string") {
				appendAssistantDelta(ev.delta);
			} else if (
				ev?.type === "thinking_delta" &&
				typeof ev.delta === "string"
			) {
				// surface thinking as a faint annotation; keep it inside the message bubble
				if (state.currentMessage) {
					state.currentMessage.contentSpan.dataset.thinking = "1";
				}
			}
			return;
		}
		case "message_end": {
			const message = record.message;
			if (message?.role === "assistant") {
				finalizeAssistantMessage();
			}
			return;
		}
		case "tool_execution_start":
			renderToolCallStart(record);
			return;
		case "tool_execution_update":
			renderToolCallUpdate(record);
			return;
		case "tool_execution_end":
			renderToolCallEnd(record);
			return;
		case "turn_end":
			appendLifecycle("turn ended", "info", "turn");
			return;
		case "agent_end":
			appendLifecycle(
				`agent settled · ${record.willRetry ? "will retry" : "done"}`,
				"info",
				"agent",
			);
			return;
		case "agent_settled":
			setRunActive(false);
			appendLifecycle("agent idle", "info", "agent");
			return;
		case "queue_update":
			if (Array.isArray(record.followUp) && record.followUp.length > 0) {
				appendLifecycle(
					`queued ${record.followUp.length} follow-up message(s)`,
					"info",
					"queue",
				);
			}
			return;
		case "auto_retry_start":
			appendLifecycle(
				`retrying (attempt ${record.attempt}/${record.maxAttempts})`,
				"info",
				"retry",
			);
			return;
		case "auto_retry_end":
			appendLifecycle(
				record.success
					? "retry succeeded"
					: `retry failed: ${record.finalError ?? "unknown"}`,
				record.success ? "info" : "error",
				"retry",
			);
			return;
		case "error":
			appendError(`agent error: ${record.error ?? "unknown"}`);
			return;
		default:
			// unknown record — already in protocol log
			return;
	}
}

function extractUserText(message) {
	if (!message) return "";
	if (typeof message.content === "string") return message.content;
	if (Array.isArray(message.content)) {
		return message.content
			.filter(
				(block) => block?.type === "text" && typeof block.text === "string",
			)
			.map((block) => block.text)
			.join("\n");
	}
	return "";
}

function handleResponse(record) {
	const id = record.id;
	const command = record.command;
	if (id && state.pendingResponseTimers.has(id)) {
		clearTimeout(state.pendingResponseTimers.get(id));
		state.pendingResponseTimers.delete(id);
	}
	if (command === "get_state" && record.success) {
		const data = record.data ?? {};
		if (data.isStreaming !== undefined) {
			setAgent(data.isStreaming ? "busy" : "idle");
		} else if (data.sessionId) {
			setAgent("idle");
		}
		if (data.cwd) appendLifecycle(`cwd: ${data.cwd}`, "info", "state");
	}
	if (!record.success) {
		appendError(`command "${command}" failed: ${record.error ?? "unknown"}`);
	}
}

function sendPrompt(message) {
	const id = nextId("prompt");
	state.runActive = true;
	state.runId = id;
	state.runStartedAt = Date.now();
	appendMessage("user", message);
	sendRecord({ id, type: "prompt", message });
	const timer = setTimeout(() => {
		state.pendingResponseTimers.delete(id);
		appendError(`prompt ${id} timed out (no response in 60s)`);
	}, 60_000);
	state.pendingResponseTimers.set(id, timer);
	setRunActive(true, id);
}

function sendAbort() {
	const id = nextId("abort");
	sendRecord({ id, type: "abort" });
	appendLifecycle("abort requested", "info", "abort");
}

function connect() {
	if (state.ws) {
		try {
			state.ws.close();
		} catch {}
	}
	const proto = location.protocol === "https:" ? "wss:" : "ws:";
	const token =
		document.querySelector('meta[name="kit-rpc-token"]')?.content ?? "";
	const ws = new WebSocket(
		`${proto}//${location.host}/ws?token=${encodeURIComponent(token)}`,
	);
	state.ws = ws;
	state.wsState = "connecting";
	setConnection("connecting");
	ws.addEventListener("open", () => {
		state.wsState = "connected";
		setConnection("connected");
		setAgent("starting");
		sendRecord({ type: "bridge.ping", t: Date.now() });
		// ask the bridge for the current state so the UI can sync.
		sendRecord({ type: "get_state" });
	});
	ws.addEventListener("close", () => {
		state.wsState = "disconnected";
		setConnection("disconnected");
		state.runActive = false;
		updateSendAvailability();
	});
	ws.addEventListener("error", () => {
		// close handler will follow
	});
	ws.addEventListener("message", (ev) => {
		const data = typeof ev.data === "string" ? ev.data : "";
		if (!data) return;
		let record;
		try {
			record = JSON.parse(data);
		} catch {
			appendError(`bridge sent invalid JSON: ${data.slice(0, 120)}`);
			return;
		}
		appendProtocolLog("rx", record);
		if (record?.type === "bridge.hello") {
			const s = record.state ?? {};
			if (s.state === "ready") setAgent("starting");
			else if (s.state === "child-exited") setAgent("exited");
			else setAgent("offline");
			if (s.pid) setChildPid(s.pid);
			return;
		}
		handleEvent(record);
	});
}

function init() {
	state.transcript = el("transcript");
	state.messageList = el("message-list");
	state.emptyState = el("transcript-empty");
	state.promptInput = el("prompt-input");
	state.composerForm = el("composer");
	state.sendButton = el("send-button");
	state.abortButton = el("abort-button");
	state.reconnectButton = el("reconnect-button");
	state.statusConnection = el("status-connection");
	state.statusAgent = el("status-agent");
	state.statusPid = el("status-pid");
	state.composerMeta = el("composer-meta");
	state.protocolLog = el("protocol-log");
	state.protocolLogBody = el("protocol-log-body");
	state.protocolLogToggle = el("protocol-log-toggle");
	state.protocolLogChevron = el("protocol-log-chevron");
	state.protocolLogCount = el("protocol-log-count");
	state.protocolLogEntries = el("protocol-log-entries");
	state.protocolLogClear = el("protocol-log-clear");
	state.protocolLogAutoscroll = el("protocol-log-autoscroll");

	state.composerForm.addEventListener("submit", (ev) => {
		ev.preventDefault();
		const value = state.promptInput.value;
		const trimmed = value.trim();
		if (!trimmed) return;
		if (state.runActive) {
			// steer instead
			const id = nextId("steer");
			sendRecord({ id, type: "steer", message: trimmed });
			state.promptInput.value = "";
			return;
		}
		state.promptInput.value = "";
		sendPrompt(trimmed);
	});

	state.promptInput.addEventListener("keydown", (ev) => {
		if (ev.key === "Enter" && !ev.shiftKey && !ev.isComposing) {
			ev.preventDefault();
			state.composerForm.requestSubmit();
		}
	});

	state.abortButton.addEventListener("click", () => {
		sendAbort();
	});

	state.reconnectButton.addEventListener("click", () => {
		connect();
	});

	state.protocolLogToggle.addEventListener("click", () => {
		const isOpen = state.protocolLogBody.parentElement.dataset.open === "true";
		if (isOpen) {
			state.protocolLogBody.parentElement.dataset.open = "false";
			state.protocolLogBody.hidden = true;
			state.protocolLogToggle.setAttribute("aria-expanded", "false");
			state.protocolLogChevron.textContent = "▸";
		} else {
			state.protocolLogBody.parentElement.dataset.open = "true";
			state.protocolLogBody.hidden = false;
			state.protocolLogToggle.setAttribute("aria-expanded", "true");
			state.protocolLogChevron.textContent = "▾";
		}
	});

	state.protocolLogClear.addEventListener("click", clearProtocolLog);

	// Cmd/Ctrl+L to toggle the protocol log
	document.addEventListener("keydown", (ev) => {
		if ((ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === "l") {
			ev.preventDefault();
			state.protocolLogToggle.click();
		}
	});

	setConnection("disconnected");
	setAgent("offline");
	setChildPid(null);
	connect();
}

document.addEventListener("DOMContentLoaded", init);
