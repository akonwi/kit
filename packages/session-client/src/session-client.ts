import {
	RPC_PROTOCOL_VERSION,
	type RpcCommand,
	type RpcResponse,
} from "@akonwi/kit-protocol";
import {
	type ClientState,
	createClientState,
	isRecord,
	ProtocolRebaseRequired,
	ProtocolSyncError,
	reduceClientRecord,
	withConnectionPhase,
} from "./client-state";
import { RemoteSessionServices } from "./remote-services";

const MAX_PENDING_COMMANDS = 256;
const DEFAULT_COMMAND_TIMEOUT_MS = 30_000;

export type SessionResumeCursor = {
	streamId: string;
	sequence: number;
};

export type SessionConnectionStartOptions = {
	cursor?: SessionResumeCursor;
	onRecord: (record: unknown) => void;
	onClose: (error?: Error) => void;
};

export interface SessionConnection {
	start(options: SessionConnectionStartOptions): Promise<void>;
	send(command: RpcCommand): void;
	close(): void;
}

export type SessionClientOptions = {
	commandTimeoutMs?: number;
	reconnect?: boolean;
};

type PendingCommand = {
	command: string;
	resolve: (response: Record<string, unknown>) => void;
	reject: (error: Error) => void;
	timer: ReturnType<typeof setTimeout>;
};

export class SessionClient {
	readonly services = new RemoteSessionServices(this);
	private stateValue = createClientState();
	private readonly listeners = new Set<(state: ClientState) => void>();
	private readonly pending = new Map<string, PendingCommand>();
	private readonly commandTimeoutMs: number;
	private readonly reconnectEnabled: boolean;
	private reconnectAttempt = 0;
	private negotiatingCapabilities = false;
	private nextCommandId = 1;
	private connecting: Promise<void> | null = null;
	private closed = false;
	private syncResolve: (() => void) | null = null;
	private syncReject: ((error: Error) => void) | null = null;
	private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

	constructor(
		readonly sessionId: string,
		private readonly connection: SessionConnection,
		options: SessionClientOptions = {},
	) {
		this.commandTimeoutMs = Math.max(
			1,
			Math.floor(options.commandTimeoutMs ?? DEFAULT_COMMAND_TIMEOUT_MS),
		);
		this.reconnectEnabled = options.reconnect !== false;
	}

	get state(): ClientState {
		return this.stateValue;
	}

	subscribe(listener: (state: ClientState) => void): () => void {
		this.listeners.add(listener);
		listener(this.stateValue);
		return () => this.listeners.delete(listener);
	}

	connect(): Promise<void> {
		if (this.closed)
			return Promise.reject(new Error("Session client is closed"));
		if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
		this.reconnectTimer = null;
		if (this.stateValue.phase === "live") return Promise.resolve();
		if (this.connecting) return this.connecting;
		this.setState(withConnectionPhase(this.stateValue, "connecting"));
		this.connecting = this.performConnect().finally(() => {
			this.connecting = null;
		});
		return this.connecting;
	}

	command(command: RpcCommand): Promise<Record<string, unknown>> {
		if (this.closed)
			return Promise.reject(new Error("Session client is closed"));
		if (
			this.stateValue.phase !== "live" &&
			!(this.negotiatingCapabilities && command.type === "get_capabilities")
		) {
			return Promise.reject(new Error("Session client is not synchronized"));
		}
		if (this.pending.size >= MAX_PENDING_COMMANDS) {
			return Promise.reject(new Error("Too many pending session commands"));
		}
		const id = `session-client:${this.nextCommandId++}`;
		return new Promise<Record<string, unknown>>((resolve, reject) => {
			const timer = setTimeout(() => {
				this.pending.delete(id);
				reject(new Error(`Session command timed out: ${command.type}`));
			}, this.commandTimeoutMs);
			this.pending.set(id, {
				command: command.type,
				resolve,
				reject,
				timer,
			});
			try {
				this.connection.send({ ...command, id });
			} catch (error) {
				clearTimeout(timer);
				this.pending.delete(id);
				reject(error instanceof Error ? error : new Error(String(error)));
			}
		});
	}

	close(): void {
		if (this.closed) return;
		this.closed = true;
		if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
		this.reconnectTimer = null;
		this.connection.close();
		this.failConnection(new Error("Session client closed"));
		this.listeners.clear();
	}

	private async performConnect(): Promise<void> {
		const synchronized = new Promise<void>((resolve, reject) => {
			this.syncResolve = resolve;
			this.syncReject = reject;
		});
		const cursor =
			this.stateValue.streamId === null
				? undefined
				: {
						streamId: this.stateValue.streamId,
						sequence: this.stateValue.sequence,
					};
		try {
			const started = this.connection.start({
				...(cursor ? { cursor } : {}),
				onRecord: (record) => this.receive(record),
				onClose: (error) => {
					this.failConnection(error ?? new Error("Session connection closed"));
					this.scheduleReconnect();
				},
			});
			await Promise.all([started, synchronized]);
			this.negotiatingCapabilities = true;
			try {
				this.services.installLimits(await this.services.fetchLimits());
			} finally {
				this.negotiatingCapabilities = false;
			}
			if (this.closed || this.stateValue.phase === "disconnected") {
				throw new Error("Session connection closed during synchronization");
			}
			this.reconnectAttempt = 0;
			this.setState(withConnectionPhase(this.stateValue, "live"));
		} catch (error) {
			const failure = error instanceof Error ? error : new Error(String(error));
			this.connection.close();
			this.failConnection(failure);
			if (!(failure instanceof ProtocolSyncError)) this.scheduleReconnect();
			throw failure;
		} finally {
			this.syncResolve = null;
			this.syncReject = null;
		}
	}

	private receive(record: unknown): void {
		try {
			if (this.resolveResponse(record)) return;
			if (isRecord(record) && record.type === "resync_required") {
				throw new ProtocolRebaseRequired(
					typeof record.reason === "string"
						? `Session resynchronization required: ${record.reason}`
						: "Session resynchronization required",
				);
			}
			if (
				isRecord(record) &&
				record.type === "sync" &&
				record.protocolVersion !== RPC_PROTOCOL_VERSION
			) {
				throw new ProtocolSyncError(
					`Unsupported RPC protocol version: ${String(record.protocolVersion)}`,
				);
			}
			if (
				isRecord(record) &&
				(record.type === "sync" || record.type === "sync_complete") &&
				record.sessionId !== this.sessionId
			) {
				throw new ProtocolSyncError("Session connection binding changed");
			}
			const next = reduceClientRecord(this.stateValue, record);
			if (isRecord(record) && record.type === "sync_complete") {
				this.syncResolve?.();
			} else if (next !== this.stateValue) {
				this.setState(next);
			}
		} catch (error) {
			const failure = error instanceof Error ? error : new Error(String(error));
			this.connection.close();
			this.failConnection(failure);
			if (failure instanceof ProtocolRebaseRequired) {
				this.setState({ ...createClientState(), lastError: failure.message });
				this.reconnectAttempt = 0;
				this.scheduleReconnect(true);
			}
		}
	}

	private resolveResponse(record: unknown): boolean {
		if (
			!isRecord(record) ||
			record.type !== "response" ||
			typeof record.id !== "string"
		) {
			return false;
		}
		const pending = this.pending.get(record.id);
		if (!pending) return true;
		this.pending.delete(record.id);
		clearTimeout(pending.timer);
		const response = record as RpcResponse;
		if (response.command !== pending.command) {
			pending.reject(
				new Error("Session response command does not match request"),
			);
		} else if (response.success !== true) {
			pending.reject(
				new Error(
					typeof response.error === "string"
						? response.error
						: `Session command failed: ${pending.command}`,
				),
			);
		} else {
			pending.resolve(record);
		}
		return true;
	}

	private scheduleReconnect(fresh = false): void {
		if (!this.reconnectEnabled || this.closed || this.reconnectTimer) return;
		const delay = fresh
			? 10
			: Math.min(5_000, 250 * 2 ** Math.min(this.reconnectAttempt++, 5));
		this.reconnectTimer = setTimeout(() => {
			this.reconnectTimer = null;
			if (this.connecting) {
				this.scheduleReconnect();
				return;
			}
			void this.connect().catch(() => this.scheduleReconnect());
		}, delay);
	}

	private failConnection(error: Error): void {
		this.syncReject?.(error);
		this.syncReject = null;
		this.syncResolve = null;
		for (const pending of this.pending.values()) {
			clearTimeout(pending.timer);
			pending.reject(error);
		}
		this.pending.clear();
		if (this.stateValue.phase !== "disconnected") {
			this.setState({
				...withConnectionPhase(this.stateValue, "disconnected"),
				lastError: error.message,
			});
		}
	}

	private setState(state: ClientState): void {
		this.stateValue = state;
		for (const listener of this.listeners) listener(state);
	}
}
