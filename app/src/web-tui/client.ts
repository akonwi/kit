import { FitAddon, Ghostty, Terminal } from "ghostty-web";
import {
	type BrowserClipboardWrite,
	parseBrowserClipboardWrite,
} from "./browser-actions";
import {
	BrowserTerminalInput,
	TerminalProtocolState,
} from "./browser-terminal-input";
import { applyBrowserTheme, parseBrowserThemeMessage } from "./browser-theme";
import { terminalInputFrames } from "./terminal-input-frames";

const RECONNECT_MIN_MS = 500;
const RECONNECT_MAX_MS = 5_000;

function statusElement(): HTMLElement | null {
	return document.getElementById("status");
}

function showStatus(text: string): void {
	const status = statusElement();
	if (!status) return;
	status.textContent = text;
	status.hidden = false;
}

function hideStatus(): void {
	const status = statusElement();
	if (status) status.hidden = true;
}

function webSocketUrl(): string {
	const scheme = location.protocol === "https:" ? "wss" : "ws";
	return `${scheme}://${location.host}/api/tui`;
}

function writeBrowserClipboardFallback(text: string): void {
	const previouslyFocused = document.activeElement;
	const textarea = document.createElement("textarea");
	textarea.value = text;
	textarea.style.position = "fixed";
	textarea.style.opacity = "0";
	document.body.append(textarea);
	textarea.select();
	try {
		if (!document.execCommand("copy")) {
			throw new Error("Browser denied clipboard access");
		}
	} finally {
		textarea.remove();
		if (previouslyFocused instanceof HTMLElement) {
			previouslyFocused.focus({ preventScroll: true });
		}
	}
}

async function writeBrowserClipboard(text: string): Promise<void> {
	if (navigator.clipboard?.writeText) {
		try {
			await navigator.clipboard.writeText(text);
			return;
		} catch {
			// Fall through for browsers or deployment contexts that deny the API.
		}
	}
	writeBrowserClipboardFallback(text);
}

function writeBrowserActionClipboard(text: string): void {
	// Server actions arrive after the terminal click's transient activation has
	// expired. Avoid permission-gated Clipboard API promises that browsers may
	// leave pending indefinitely; the synchronous copy command either succeeds
	// or can be acknowledged as denied immediately.
	writeBrowserClipboardFallback(text);
}

class TuiConnection {
	private socket: WebSocket | null = null;
	private reconnectDelay = RECONNECT_MIN_MS;
	private reconnectTimer: number | null = null;
	private closedByPage = false;

	constructor(
		private readonly terminal: Terminal,
		private readonly fit: FitAddon,
		private readonly protocol: TerminalProtocolState,
	) {
		terminal.onData((data) => this.sendInput(data));
		terminal.onResize(({ cols, rows }) =>
			this.sendControl("resize", cols, rows),
		);
		terminal.onTitleChange((title) => {
			document.title = title || "Kit (terminal)";
		});
		window.addEventListener("pagehide", () => {
			this.closedByPage = true;
			if (this.reconnectTimer !== null) clearTimeout(this.reconnectTimer);
			this.reconnectTimer = null;
			this.socket?.close(1000, "page closed");
		});
		window.addEventListener("pageshow", (event) => {
			if (!event.persisted) return;
			this.closedByPage = false;
			if (!this.socket) this.connect();
		});
	}

	connect(): void {
		if (this.socket) return;
		showStatus("connecting…");
		const socket = new WebSocket(webSocketUrl());
		socket.binaryType = "arraybuffer";
		this.socket = socket;
		socket.addEventListener("open", () => {
			this.reconnectDelay = RECONNECT_MIN_MS;
			hideStatus();
			this.fit.fit();
			this.sendControl("init", this.terminal.cols, this.terminal.rows);
			this.terminal.focus();
		});
		socket.addEventListener("message", (event) => {
			if (event.data instanceof ArrayBuffer) {
				const bytes = new Uint8Array(event.data);
				this.protocol.feed(bytes);
				this.terminal.write(bytes);
				return;
			}
			if (typeof event.data === "string") {
				const theme = parseBrowserThemeMessage(event.data);
				if (theme) {
					applyBrowserTheme(theme);
					return;
				}
				const clipboard = parseBrowserClipboardWrite(event.data);
				if (clipboard) void this.writeClipboard(socket, clipboard);
			}
		});
		socket.addEventListener("close", (event) => {
			if (this.socket !== socket) return;
			this.socket = null;
			if (this.closedByPage) return;
			if (event.code === 4001) {
				showStatus("disconnected — another tab took over this terminal");
				return;
			}
			this.scheduleReconnect();
		});
		socket.addEventListener("error", () => socket.close());
	}

	private scheduleReconnect(): void {
		showStatus("disconnected — reconnecting…");
		const delay = this.reconnectDelay;
		this.reconnectDelay = Math.min(RECONNECT_MAX_MS, delay * 2);
		this.reconnectTimer = window.setTimeout(() => {
			this.reconnectTimer = null;
			this.connect();
		}, delay);
	}

	sendInput(data: string): void {
		if (this.socket?.readyState !== WebSocket.OPEN) return;
		for (const frame of terminalInputFrames(data)) this.socket.send(frame);
	}

	private writeClipboard(
		socket: WebSocket,
		action: BrowserClipboardWrite,
	): void {
		try {
			writeBrowserActionClipboard(action.text);
			this.sendClipboardResult(socket, action.id, true);
		} catch (error) {
			this.sendClipboardResult(
				socket,
				action.id,
				false,
				error instanceof Error ? error.message : String(error),
			);
		}
	}

	private sendClipboardResult(
		socket: WebSocket,
		id: number,
		ok: boolean,
		error?: string,
	): void {
		if (socket.readyState !== WebSocket.OPEN) return;
		socket.send(
			JSON.stringify({
				type: "clipboard-result",
				id,
				ok,
				...(error ? { error: error.slice(0, 256) } : {}),
			}),
		);
	}

	private sendControl(
		type: "init" | "resize",
		cols: number,
		rows: number,
	): void {
		if (this.socket?.readyState === WebSocket.OPEN) {
			this.socket.send(JSON.stringify({ type, cols, rows }));
		}
	}
}

async function main(): Promise<void> {
	const element = document.getElementById("terminal");
	if (!element) throw new Error("terminal element missing");
	const ghostty = await Ghostty.load("/assets/ghostty-vt.wasm");
	const terminal = new Terminal({
		ghostty,
		cursorBlink: true,
		fontFamily: '"JetBrains Mono", ui-monospace, SFMono-Regular, monospace',
		fontSize: 14,
		scrollback: 10_000,
		theme: {
			background: "#0a0a0a",
			foreground: "#fafafa",
			cursor: "#fafafa",
			selectionBackground: "#404040",
		},
	});
	const fit = new FitAddon();
	terminal.loadAddon(fit);
	terminal.open(element);
	fit.observeResize();
	fit.fit();
	const protocol = new TerminalProtocolState();
	const connection = new TuiConnection(terminal, fit, protocol);
	new BrowserTerminalInput({
		root: element,
		protocol,
		geometry: () => {
			const canvas = element.querySelector("canvas");
			if (!canvas) return null;
			return {
				columns: terminal.cols,
				rows: terminal.rows,
				bounds: canvas.getBoundingClientRect(),
			};
		},
		send: (data) => connection.sendInput(data),
		focus: () => terminal.focus(),
		copySelection: () => terminal.getSelection(),
		writeClipboard: writeBrowserClipboard,
	});
	connection.connect();
}

void main().catch((error) => {
	showStatus(
		`failed to start terminal: ${error instanceof Error ? error.message : String(error)}`,
	);
});
