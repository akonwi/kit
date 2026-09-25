import {
	isRpcCommand,
	RPC_PROTOCOL_VERSION,
	type RpcCommand,
} from "@akonwi/kit-protocol";
import type { ServerWebSocket } from "bun";
import type { KitServerConnection } from "./kit-server";
import { RemoteEventJournal } from "./remote-event-journal";
import {
	MAX_RPC_WIRE_SNAPSHOT_MESSAGES,
	RpcWireProjection,
} from "./rpc-wire-projection";

export type LocalSessionResumeCursor = {
	requested: boolean;
	valid: boolean;
	streamId?: string;
	after?: number;
};

export type LocalSessionSocketData = {
	channel: LocalSessionRpcChannel;
	resume: LocalSessionResumeCursor;
	pendingCommands: number;
};

const MAX_LOCAL_SESSION_CLIENTS = 16;
const MAX_LOCAL_PENDING_COMMANDS_PER_CLIENT = 32;
const MAX_LOCAL_PENDING_COMMANDS = 128;

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

function canonicalJson(value: unknown): string {
	const serialized = JSON.stringify(value, (_key, nested) =>
		typeof nested === "bigint" ? nested.toString() : nested,
	);
	if (serialized === undefined)
		throw new Error("RPC record is not serializable");
	return serialized;
}

function parseCommand(message: string | Buffer): RpcCommand {
	const value: unknown = JSON.parse(
		typeof message === "string" ? message : message.toString("utf8"),
	);
	if (!isRpcCommand(value)) {
		throw new Error("Command must be an object with a string type");
	}
	return value;
}

export function parseLocalSessionResumeCursor(
	url: URL,
): LocalSessionResumeCursor {
	const streamId = url.searchParams.get("streamId");
	const afterValue = url.searchParams.get("after");
	if (streamId === null && afterValue === null) {
		return { requested: false, valid: true };
	}
	if (
		!streamId ||
		streamId.length > 128 ||
		afterValue === null ||
		!/^[0-9]+$/.test(afterValue)
	) {
		return { requested: true, valid: false };
	}
	const after = Number(afterValue);
	return Number.isSafeInteger(after)
		? { requested: true, valid: true, streamId, after }
		: { requested: true, valid: false };
}

export class LocalSessionRpcChannel {
	private readonly clients = new Set<ServerWebSocket<LocalSessionSocketData>>();
	private readonly journal = new RemoteEventJournal();
	private readonly projection = new RpcWireProjection();
	private readonly unsubscribe: () => void;
	private disposed = false;
	private pendingCommands = 0;

	constructor(private readonly connection: KitServerConnection) {
		this.unsubscribe = connection.subscribe((record) => {
			this.broadcast(record);
		});
	}

	get sessionId(): string {
		return this.connection.sessionId;
	}

	open(socket: ServerWebSocket<LocalSessionSocketData>): void {
		if (this.disposed) {
			socket.close(1012, "Session channel is shutting down");
			return;
		}
		if (this.clients.size >= MAX_LOCAL_SESSION_CLIENTS) {
			socket.close(1013, "Session client limit reached");
			return;
		}
		if (this.synchronize(socket)) this.clients.add(socket);
	}

	message(
		socket: ServerWebSocket<LocalSessionSocketData>,
		message: string | Buffer,
	): void {
		let command: RpcCommand;
		try {
			command = parseCommand(message);
		} catch (error) {
			this.send(socket, {
				type: "response",
				command: "parse",
				success: false,
				error: `Failed to parse command: ${errorMessage(error)}`,
			});
			return;
		}
		if (command.type === "get_message_chunk") {
			this.send(socket, this.projection.messageChunkResponse(command));
			return;
		}
		if (
			socket.data.pendingCommands >= MAX_LOCAL_PENDING_COMMANDS_PER_CLIENT ||
			this.pendingCommands >= MAX_LOCAL_PENDING_COMMANDS
		) {
			this.send(socket, {
				...(command.id === undefined ? {} : { id: command.id }),
				type: "response",
				command: command.type,
				success: false,
				error: "Too many pending session commands",
			});
			return;
		}
		const prepared = this.projection.prepareCommand(command);
		socket.data.pendingCommands += 1;
		this.pendingCommands += 1;
		void this.connection
			.handleCommand(prepared, async (record) => {
				this.send(socket, this.prepareResponse(prepared, record));
			})
			.catch((error) => {
				this.send(socket, {
					...(command.id === undefined ? {} : { id: command.id }),
					type: "response",
					command: command.type,
					success: false,
					error: errorMessage(error),
				});
			})
			.finally(() => {
				socket.data.pendingCommands = Math.max(
					0,
					socket.data.pendingCommands - 1,
				);
				this.pendingCommands = Math.max(0, this.pendingCommands - 1);
			});
	}

	close(socket: ServerWebSocket<LocalSessionSocketData>): void {
		this.clients.delete(socket);
	}

	dispose(): void {
		if (this.disposed) return;
		this.disposed = true;
		this.unsubscribe();
		for (const socket of this.clients) {
			socket.close(1001, "Session channel closed");
		}
		this.clients.clear();
		this.connection.close();
	}

	private broadcast(record: unknown): void {
		if (this.disposed) return;
		let event: Record<string, unknown>;
		try {
			event = this.journal.append(this.projection.projectRecord(record));
		} catch {
			event = this.journal.append({
				type: "resync_required",
				reason: "event_projection_failed",
			});
		}
		for (const socket of [...this.clients]) this.send(socket, event);
	}

	private prepareResponse(command: RpcCommand, record: unknown): unknown {
		const prepared = this.projection.prepareResponse(command, record);
		if (
			command.type !== "get_capabilities" ||
			!isRecord(prepared) ||
			prepared.success !== true ||
			!isRecord(prepared.data)
		) {
			return prepared;
		}
		return {
			...prepared,
			data: {
				...prepared.data,
				eventSequencing: {
					supported: true,
					resume: "websocket_query",
					streamId: this.journal.streamId,
					latestSequence: this.journal.latestSequence,
					...this.journal.retention,
				},
			},
		};
	}

	private synchronize(
		socket: ServerWebSocket<LocalSessionSocketData>,
	): boolean {
		const cursor = socket.data.resume;
		const latestSequence = this.journal.latestSequence;
		if (
			cursor.requested &&
			cursor.valid &&
			cursor.streamId === this.journal.streamId &&
			cursor.after !== undefined &&
			cursor.after <= latestSequence
		) {
			const replay = this.journal.replayAfter(cursor.after);
			if (replay) {
				if (
					!this.send(socket, {
						type: "sync",
						mode: "replay",
						protocolVersion: RPC_PROTOCOL_VERSION,
						sessionId: this.sessionId,
						streamId: this.journal.streamId,
						sequence: cursor.after,
						targetSequence: latestSequence,
					})
				) {
					return false;
				}
				for (const event of replay) {
					if (!this.send(socket, event)) return false;
				}
				return this.send(socket, {
					type: "sync_complete",
					mode: "replay",
					sessionId: this.sessionId,
					streamId: this.journal.streamId,
					sequence: latestSequence,
				});
			}
		}

		let reason = "initial";
		if (cursor.requested && !cursor.valid) reason = "invalid_cursor";
		else if (cursor.requested && cursor.streamId !== this.journal.streamId) {
			reason = "stream_changed";
		} else if (
			cursor.requested &&
			cursor.after !== undefined &&
			cursor.after > latestSequence
		) {
			reason = "invalid_cursor";
		} else if (cursor.requested) {
			reason = "history_unavailable";
		}

		const snapshot = this.projection.boundedConnectionSnapshot(
			this.connection.getConnectionSnapshot(MAX_RPC_WIRE_SNAPSHOT_MESSAGES),
		);
		if (
			!this.send(socket, {
				type: "sync",
				mode: "snapshot",
				reason,
				protocolVersion: RPC_PROTOCOL_VERSION,
				sessionId: this.sessionId,
				streamId: this.journal.streamId,
				sequence: latestSequence,
				...snapshot,
			})
		) {
			return false;
		}
		return this.send(socket, {
			type: "sync_complete",
			mode: "snapshot",
			sessionId: this.sessionId,
			streamId: this.journal.streamId,
			sequence: latestSequence,
		});
	}

	private send(
		socket: ServerWebSocket<LocalSessionSocketData>,
		record: unknown,
	): boolean {
		if (socket.readyState !== WebSocket.OPEN) {
			this.clients.delete(socket);
			return false;
		}
		try {
			const status = socket.send(
				canonicalJson(this.projection.projectRecord(record)),
			);
			if (status > 0) return true;
		} catch {
			// Termination below also removes a socket that cannot serialize a record.
		}
		this.clients.delete(socket);
		socket.terminate();
		return false;
	}
}
