import type {
	AgentRuntime,
	AgentRuntimeEvent,
} from "../../runtime/agent-runtime";
import { openExternal } from "../../shell/open-external";

type CodeReviewClientMessage =
	| { type: "ready" }
	| { type: "request_state" }
	| { type: "ping"; text?: string }
	| { type: "submit_note"; text: string };

type CodeReviewServerMessage =
	| { type: "connected"; sessionUrl: string }
	| { type: "state"; state: CodeReviewBrowserState; reason: string }
	| { type: "host_log"; level: "info" | "error"; message: string; at: string }
	| { type: "ack"; action: string; detail: string; at: string };

type CodeReviewBrowserState = {
	sessionId: string;
	sessionName: string | null;
	cwd: string;
	model: string | null;
	turnCount: number;
	pendingMessages: string[];
	openedAt: string;
	lastUpdatedAt: string;
};

type SocketData = {
	connectedAt: string;
};

class CodeReviewBrowserHost {
	private server: Bun.Server<SocketData> | null = null;
	private readonly clients = new Set<Bun.ServerWebSocket<SocketData>>();
	private token = crypto.randomUUID();
	private activeRuntime: AgentRuntime | null = null;
	private unsubscribeRuntime: (() => void) | null = null;
	private state: CodeReviewBrowserState | null = null;
	private openedAt = new Date().toISOString();

	async launch(runtime: AgentRuntime): Promise<void> {
		this.ensureServer();
		this.attachRuntime(runtime);
		this.openedAt = new Date().toISOString();
		this.state = this.buildState(runtime);
		this.broadcast({
			type: "host_log",
			level: "info",
			message: "Code review browser session started from Kit.",
			at: new Date().toISOString(),
		});
		this.broadcastState("launch");

		const url = this.getSessionUrl();
		await openExternal(url);
		runtime.emitInfo("Code review opened", [url]);
	}

	private ensureServer(): void {
		if (this.server) return;

		this.server = Bun.serve<SocketData>({
			port: 0,
			fetch: (request, server) => {
				const url = new URL(request.url);
				if (!this.isAuthorized(url)) {
					return new Response("Unauthorized", { status: 401 });
				}

				if (url.pathname === "/ws") {
					const upgraded = server.upgrade(request, {
						data: { connectedAt: new Date().toISOString() },
					});
					if (upgraded) return undefined;
					return new Response("WebSocket upgrade failed", { status: 500 });
				}

				if (url.pathname === "/") {
					return new Response(this.renderHtml(), {
						headers: {
							"content-type": "text/html; charset=utf-8",
							"cache-control": "no-store",
						},
					});
				}

				if (url.pathname === "/health") {
					return Response.json({ ok: true });
				}

				return new Response("Not found", { status: 404 });
			},
			websocket: {
				open: (ws) => {
					this.clients.add(ws);
					this.send(ws, {
						type: "connected",
						sessionUrl: this.getSessionUrl(),
					});
					this.send(ws, {
						type: "host_log",
						level: "info",
						message: `Browser connected at ${ws.data.connectedAt}.`,
						at: new Date().toISOString(),
					});
					this.sendState(ws, "socket_open");
				},
				close: (ws) => {
					this.clients.delete(ws);
				},
				message: (ws, message) => {
					const text =
						typeof message === "string"
							? message
							: Buffer.from(message).toString("utf8");
					this.handleClientMessage(ws, text);
				},
			},
		});
	}

	private attachRuntime(runtime: AgentRuntime): void {
		if (this.activeRuntime === runtime && this.unsubscribeRuntime) return;

		this.unsubscribeRuntime?.();
		this.activeRuntime = runtime;
		this.unsubscribeRuntime = runtime.subscribe((event) => {
			this.handleRuntimeEvent(runtime, event);
		});
	}

	private handleRuntimeEvent(
		runtime: AgentRuntime,
		event: AgentRuntimeEvent,
	): void {
		if (this.activeRuntime !== runtime) return;
		this.state = this.buildState(runtime);

		switch (event.type) {
			case "info":
				this.broadcast({
					type: "host_log",
					level: "info",
					message: [event.title, ...event.lines].filter(Boolean).join(" — "),
					at: new Date().toISOString(),
				});
				break;
			case "error":
				this.broadcast({
					type: "host_log",
					level: "error",
					message: [event.title, ...event.lines].filter(Boolean).join(" — "),
					at: new Date().toISOString(),
				});
				break;
			case "panel":
				this.broadcast({
					type: "host_log",
					level: "info",
					message: event.panel.pending
						? `Kit panel: ${event.panel.title}`
						: "Kit panel cleared",
					at: new Date().toISOString(),
				});
				break;
			default:
				break;
		}

		if (
			event.type === "session_changed" ||
			event.type === "session_updated" ||
			event.type === "turns_changed" ||
			event.type === "pending_messages_changed" ||
			event.type === "pending_changed" ||
			event.type === "status_changed" ||
			event.type === "turn_complete"
		) {
			this.broadcastState(event.type);
		}
	}

	private handleClientMessage(
		ws: Bun.ServerWebSocket<SocketData>,
		payload: string,
	): void {
		const runtime = this.activeRuntime;
		if (!runtime) {
			this.send(ws, {
				type: "host_log",
				level: "error",
				message: "No active Kit runtime is attached.",
				at: new Date().toISOString(),
			});
			return;
		}

		let message: CodeReviewClientMessage;
		try {
			message = JSON.parse(payload) as CodeReviewClientMessage;
		} catch {
			this.send(ws, {
				type: "host_log",
				level: "error",
				message: `Invalid JSON from browser: ${payload}`,
				at: new Date().toISOString(),
			});
			return;
		}

		switch (message.type) {
			case "ready":
				this.send(ws, {
					type: "ack",
					action: "ready",
					detail: "Kit received browser ready signal.",
					at: new Date().toISOString(),
				});
				this.sendState(ws, "browser_ready");
				break;
			case "request_state":
				this.sendState(ws, "browser_request");
				break;
			case "ping": {
				const detail = message.text?.trim() || "Ping received by Kit.";
				runtime.emitInfo("Code review browser", [detail]);
				this.broadcast({
					type: "ack",
					action: "ping",
					detail,
					at: new Date().toISOString(),
				});
				break;
			}
			case "submit_note": {
				const note = message.text.trim();
				if (note.length === 0) {
					this.send(ws, {
						type: "host_log",
						level: "error",
						message: "Cannot send an empty note to Kit.",
						at: new Date().toISOString(),
					});
					return;
				}
				runtime.emitInfo("Code review browser note", [note]);
				this.broadcast({
					type: "ack",
					action: "submit_note",
					detail: `Kit received note: ${note}`,
					at: new Date().toISOString(),
				});
				break;
			}
		}
	}

	private buildState(runtime: AgentRuntime): CodeReviewBrowserState {
		const session = runtime.getSession();
		return {
			sessionId: session.id,
			sessionName: session.name ?? null,
			cwd: session.cwd,
			model: session.model ?? null,
			turnCount: session.turns.length,
			pendingMessages: runtime.getPendingMessages(),
			openedAt: this.openedAt,
			lastUpdatedAt: new Date().toISOString(),
		};
	}

	private sendState(ws: Bun.ServerWebSocket<SocketData>, reason: string): void {
		if (!this.state) return;
		this.send(ws, { type: "state", state: this.state, reason });
	}

	private broadcastState(reason: string): void {
		if (!this.state) return;
		this.broadcast({ type: "state", state: this.state, reason });
	}

	private broadcast(message: CodeReviewServerMessage): void {
		for (const client of this.clients) {
			this.send(client, message);
		}
	}

	private send(
		ws: Bun.ServerWebSocket<SocketData>,
		message: CodeReviewServerMessage,
	): void {
		ws.send(JSON.stringify(message));
	}

	private getSessionUrl(): string {
		const server = this.server;
		if (!server) {
			throw new Error("Code review browser server has not started.");
		}
		return `http://127.0.0.1:${server.port}/?token=${this.token}`;
	}

	private isAuthorized(url: URL): boolean {
		return url.searchParams.get("token") === this.token;
	}

	private renderHtml(): string {
		return `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>Kit Code Review</title>
	<style>
		:root {
			color-scheme: dark;
			font-family: Inter, ui-sans-serif, system-ui, sans-serif;
			background: #0b1020;
			color: #e5e7eb;
		}
		body {
			margin: 0;
			padding: 24px;
			background: radial-gradient(circle at top, #18213f, #0b1020 55%);
		}
		main {
			max-width: 1080px;
			margin: 0 auto;
			display: grid;
			gap: 16px;
		}
		.card {
			background: rgba(15, 23, 42, 0.88);
			border: 1px solid rgba(148, 163, 184, 0.2);
			border-radius: 16px;
			padding: 16px;
			box-shadow: 0 12px 40px rgba(0, 0, 0, 0.25);
		}
		h1 {
			margin: 0;
			font-size: 28px;
		}
		p, pre {
			margin: 0;
		}
		.muted {
			color: #94a3b8;
		}
		.status {
			display: inline-flex;
			gap: 8px;
			align-items: center;
			padding: 6px 10px;
			border-radius: 999px;
			background: rgba(30, 41, 59, 0.9);
			border: 1px solid rgba(148, 163, 184, 0.2);
			font-size: 14px;
		}
		.grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
			gap: 16px;
		}
		.actions {
			display: flex;
			flex-wrap: wrap;
			gap: 12px;
		}
		button {
			border: 0;
			border-radius: 10px;
			padding: 10px 14px;
			font: inherit;
			cursor: pointer;
			background: #2563eb;
			color: white;
		}
		button.secondary {
			background: #334155;
		}
		textarea {
			width: 100%;
			min-height: 110px;
			border-radius: 12px;
			border: 1px solid rgba(148, 163, 184, 0.25);
			background: #020617;
			color: #e5e7eb;
			padding: 12px;
			font: inherit;
			resize: vertical;
			box-sizing: border-box;
		}
		pre {
			white-space: pre-wrap;
			word-break: break-word;
			background: rgba(2, 6, 23, 0.85);
			border-radius: 12px;
			padding: 12px;
			max-height: 340px;
			overflow: auto;
			font-size: 13px;
		}
		ul {
			list-style: none;
			margin: 0;
			padding: 0;
			display: grid;
			gap: 8px;
		}
		li {
			padding: 10px 12px;
			border-radius: 10px;
			background: rgba(2, 6, 23, 0.7);
			border: 1px solid rgba(148, 163, 184, 0.14);
		}
		li.error {
			border-color: rgba(248, 113, 113, 0.35);
		}
	</style>
</head>
<body>
	<main>
		<section class="card">
			<div style="display:flex;justify-content:space-between;gap:12px;align-items:flex-start;flex-wrap:wrap;">
				<div style="display:grid;gap:8px;">
					<h1>Kit code review prototype</h1>
					<p class="muted">Local browser prototype for the future <code>/code-review</code> flow.</p>
				</div>
				<div class="status"><span id="connection-dot">●</span><span id="connection-text">Connecting…</span></div>
			</div>
		</section>

		<section class="grid">
			<div class="card" style="display:grid;gap:12px;">
				<h2 style="margin:0;font-size:18px;">Session state</h2>
				<pre id="state-view">Waiting for Kit…</pre>
				<div class="actions">
					<button id="request-state">Request latest state</button>
					<button class="secondary" id="send-ping">Send ping to Kit</button>
				</div>
			</div>
			<div class="card" style="display:grid;gap:12px;">
				<h2 style="margin:0;font-size:18px;">Browser → Kit</h2>
				<textarea id="note-input" placeholder="Write a test message for Kit..."></textarea>
				<div class="actions">
					<button id="send-note">Send note to Kit</button>
				</div>
				<p class="muted">For now this just proves the host bridge; later this becomes structured review state.</p>
			</div>
		</section>

		<section class="card" style="display:grid;gap:12px;">
			<h2 style="margin:0;font-size:18px;">Kit event log</h2>
			<ul id="log-list"></ul>
		</section>
	</main>

	<script>
		const token = new URL(window.location.href).searchParams.get('token');
		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const socketUrl = protocol + '//' + window.location.host + '/ws?token=' + encodeURIComponent(token ?? '');
		const stateView = document.getElementById('state-view');
		const logList = document.getElementById('log-list');
		const connectionText = document.getElementById('connection-text');
		const connectionDot = document.getElementById('connection-dot');
		const noteInput = document.getElementById('note-input');

		function addLog(text, level = 'info') {
			const item = document.createElement('li');
			if (level === 'error') item.classList.add('error');
			item.textContent = text;
			logList.prepend(item);
		}

		function setConnection(status, color) {
			connectionText.textContent = status;
			connectionDot.style.color = color;
		}

		const socket = new WebSocket(socketUrl);

		socket.addEventListener('open', () => {
			setConnection('Connected to Kit', '#22c55e');
			addLog('WebSocket connected to Kit host.');
			socket.send(JSON.stringify({ type: 'ready' }));
		});

		socket.addEventListener('close', () => {
			setConnection('Disconnected', '#f97316');
			addLog('Connection to Kit closed.', 'error');
		});

		socket.addEventListener('error', () => {
			setConnection('Connection error', '#ef4444');
			addLog('Connection error while talking to Kit.', 'error');
		});

		socket.addEventListener('message', (event) => {
			const msg = JSON.parse(event.data);
			if (msg.type === 'state') {
				stateView.textContent = JSON.stringify(msg.state, null, 2);
				addLog('State update from Kit (' + msg.reason + ').');
				return;
			}
			if (msg.type === 'host_log') {
				addLog(msg.message, msg.level);
				return;
			}
			if (msg.type === 'ack') {
				addLog('Ack: ' + msg.action + ' — ' + msg.detail);
				return;
			}
			if (msg.type === 'connected') {
				addLog('Connected to ' + msg.sessionUrl);
			}
		});

		document.getElementById('request-state').addEventListener('click', () => {
			socket.send(JSON.stringify({ type: 'request_state' }));
		});

		document.getElementById('send-ping').addEventListener('click', () => {
			socket.send(JSON.stringify({ type: 'ping', text: 'Hello from the browser prototype.' }));
		});

		document.getElementById('send-note').addEventListener('click', () => {
			const text = noteInput.value.trim();
			socket.send(JSON.stringify({ type: 'submit_note', text }));
			if (text.length > 0) noteInput.value = '';
		});
	</script>
</body>
</html>`;
	}
}

export const codeReviewBrowserHost = new CodeReviewBrowserHost();
