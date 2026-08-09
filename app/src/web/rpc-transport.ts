type PendingCommand = {
	resolve(record: Record<string, unknown>): void;
	reject(error: Error): void;
};

export type ResumeCursor = {
	streamId: string;
	sequence: number;
};

export type RpcTransportHooks = {
	getResumeCursor(): ResumeCursor | null;
	onConnecting(): void;
	onDisconnected(): void;
	onProtocolRecords(records: readonly unknown[]): void;
	onError(error: unknown): void;
};

export interface RpcCommandClient {
	command(command: Record<string, unknown>): Promise<Record<string, unknown>>;
}

export class WebSocketRpcTransport implements RpcCommandClient {
	private socket: WebSocket | null = null;
	private reconnectTimer: number | null = null;
	private reconnectAttempt = 0;
	private requireSnapshot = false;
	private commandCounter = 0;
	private readonly pendingCommands = new Map<string, PendingCommand>();
	private readonly queuedRecords: unknown[] = [];
	private protocolTimer: number | null = null;
	private started = false;

	private readonly onlineListener = () => {
		if (!this.socket && this.reconnectTimer === null) this.connect();
	};

	constructor(private readonly hooks: RpcTransportHooks) {}

	start(): void {
		if (this.started) return;
		this.started = true;
		window.addEventListener("online", this.onlineListener);
		this.connect();
	}

	dispose(): void {
		this.started = false;
		window.removeEventListener("online", this.onlineListener);
		if (this.reconnectTimer !== null) clearTimeout(this.reconnectTimer);
		if (this.protocolTimer !== null) clearTimeout(this.protocolTimer);
		this.reconnectTimer = null;
		this.protocolTimer = null;
		this.socket?.close();
		this.socket = null;
		this.rejectPendingCommands(new Error("Web client disposed"));
	}

	isOpen(): boolean {
		return this.socket?.readyState === WebSocket.OPEN;
	}

	resetReconnectBackoff(): void {
		this.reconnectAttempt = 0;
	}

	acceptSnapshot(): void {
		this.requireSnapshot = false;
	}

	command(command: Record<string, unknown>): Promise<Record<string, unknown>> {
		if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
			return Promise.reject(new Error("Kit is not connected"));
		}
		this.commandCounter += 1;
		const id = `web-${Date.now().toString(36)}-${this.commandCounter.toString(36)}`;
		return new Promise((resolve, reject) => {
			this.pendingCommands.set(id, { resolve, reject });
			this.socket?.send(JSON.stringify({ ...command, id }));
		});
	}

	private reconnectUrl(): string {
		const url = new URL("/api/rpc", window.location.href);
		url.protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
		const cursor = this.requireSnapshot ? null : this.hooks.getResumeCursor();
		if (cursor) {
			url.searchParams.set("streamId", cursor.streamId);
			url.searchParams.set("after", String(cursor.sequence));
		}
		return url.href;
	}

	private scheduleReconnect(): void {
		if (!this.started || this.reconnectTimer !== null) return;
		const delay = Math.min(500 * 2 ** this.reconnectAttempt, 10_000);
		this.reconnectAttempt += 1;
		this.reconnectTimer = window.setTimeout(() => {
			this.reconnectTimer = null;
			this.connect();
		}, delay);
	}

	drainProtocolRecords(): boolean {
		if (this.protocolTimer !== null) clearTimeout(this.protocolTimer);
		this.protocolTimer = null;
		if (this.queuedRecords.length === 0) return true;
		const records = this.queuedRecords.splice(0);
		try {
			this.hooks.onProtocolRecords(records);
			return true;
		} catch (error) {
			this.requireSnapshot = true;
			this.hooks.onError(error);
			this.rejectPendingCommands(
				error instanceof Error ? error : new Error(String(error)),
			);
			this.socket?.close();
			return false;
		}
	}

	private enqueueProtocolRecord(record: unknown): void {
		this.queuedRecords.push(record);
		if (this.queuedRecords.length >= 256) {
			this.drainProtocolRecords();
			return;
		}
		if (this.protocolTimer === null) {
			this.protocolTimer = window.setTimeout(
				() => this.drainProtocolRecords(),
				0,
			);
		}
	}

	private connect(): void {
		if (!this.started) return;
		this.hooks.onConnecting();
		const nextSocket = new WebSocket(this.reconnectUrl());
		this.socket = nextSocket;
		nextSocket.addEventListener("message", (event) => {
			if (this.socket !== nextSocket) return;
			try {
				const record: unknown = JSON.parse(String(event.data));
				if (isResponse(record)) {
					if (this.queuedRecords.length > 0 && !this.drainProtocolRecords()) {
						return;
					}
					const id = record.id;
					if (typeof id === "string") {
						const pending = this.pendingCommands.get(id);
						if (pending) {
							this.pendingCommands.delete(id);
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
				this.enqueueProtocolRecord(record);
			} catch (error) {
				this.requireSnapshot = true;
				this.hooks.onError(error);
				nextSocket.close();
			}
		});
		nextSocket.addEventListener("close", () => {
			if (this.socket !== nextSocket) return;
			this.socket = null;
			this.queuedRecords.length = 0;
			if (this.protocolTimer !== null) clearTimeout(this.protocolTimer);
			this.protocolTimer = null;
			this.rejectPendingCommands(
				new Error("Connection closed before a response arrived"),
			);
			this.hooks.onDisconnected();
			this.scheduleReconnect();
		});
		nextSocket.addEventListener("error", () => {
			this.hooks.onError(new Error("Unable to connect to the Kit session."));
		});
	}

	private rejectPendingCommands(error: Error): void {
		for (const pending of this.pendingCommands.values()) pending.reject(error);
		this.pendingCommands.clear();
	}
}

function isResponse(value: unknown): value is Record<string, unknown> & {
	type: "response";
} {
	return (
		typeof value === "object" &&
		value !== null &&
		!Array.isArray(value) &&
		(value as Record<string, unknown>).type === "response"
	);
}
