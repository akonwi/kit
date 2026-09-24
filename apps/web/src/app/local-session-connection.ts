import type { RpcCommand } from "@akonwi/kit-protocol";
import type {
	SessionConnection,
	SessionConnectionStartOptions,
} from "@akonwi/kit-session-client";
import {
	getLocalServerPaths,
	LOCAL_SERVER_USERNAME,
	type LocalServerPaths,
	type LocalServerRegistration,
	readLocalServerPassword,
	readLocalServerRegistration,
} from "./local-server-files";

export type LocalSessionEndpoint = {
	registration: LocalServerRegistration;
	password: string;
};

export type LocalSessionConnectionOptions = { sessionId: string } & (
	| LocalSessionEndpoint
	| { resolveEndpoint: () => Promise<LocalSessionEndpoint> }
);

type WebSocketWithOptions = new (
	url: string,
	options: Bun.WebSocketOptions,
) => WebSocket;

function authorization(password: string): string {
	return `Basic ${Buffer.from(`${LOCAL_SERVER_USERNAME}:${password}`, "utf8").toString("base64")}`;
}

export async function resolveLocalSessionEndpoint(
	paths: LocalServerPaths = getLocalServerPaths(),
): Promise<LocalSessionEndpoint> {
	const [registration, password] = await Promise.all([
		readLocalServerRegistration(paths),
		readLocalServerPassword(paths),
	]);
	if (!registration || !password) {
		throw new Error("Local Kit server registration is unavailable");
	}
	return { registration, password };
}

export class LocalSessionConnection implements SessionConnection {
	private socket: WebSocket | null = null;
	private starting: Promise<void> | null = null;
	private callbacks: SessionConnectionStartOptions | null = null;
	private explicitClose = false;
	private attemptGeneration = 0;

	constructor(private readonly options: LocalSessionConnectionOptions) {}

	start(options: SessionConnectionStartOptions): Promise<void> {
		if (this.socket?.readyState === WebSocket.OPEN) {
			return Promise.reject(
				new Error("Local session connection is already open"),
			);
		}
		if (this.starting) return this.starting;
		this.explicitClose = false;
		this.callbacks = options;
		const generation = ++this.attemptGeneration;
		this.starting = this.open(options, generation).finally(() => {
			this.starting = null;
		});
		return this.starting;
	}

	send(command: RpcCommand): void {
		if (this.socket?.readyState !== WebSocket.OPEN) {
			throw new Error("Local session connection is not open");
		}
		this.socket.send(JSON.stringify(command));
	}

	close(): void {
		this.explicitClose = true;
		this.attemptGeneration += 1;
		this.callbacks = null;
		const socket = this.socket;
		this.socket = null;
		if (
			socket?.readyState === WebSocket.OPEN ||
			socket?.readyState === WebSocket.CONNECTING
		) {
			socket.close(1000, "Client closed");
		}
	}

	private async open(
		options: SessionConnectionStartOptions,
		generation: number,
	): Promise<void> {
		const endpoint =
			"resolveEndpoint" in this.options
				? await this.options.resolveEndpoint()
				: this.options;
		if (this.explicitClose || generation !== this.attemptGeneration) {
			throw new Error("Local session connection attempt was cancelled");
		}
		const url = new URL(endpoint.registration.url);
		url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
		url.pathname = `/api/sessions/${encodeURIComponent(this.options.sessionId)}/rpc`;
		if (options.cursor) {
			url.searchParams.set("streamId", options.cursor.streamId);
			url.searchParams.set("after", String(options.cursor.sequence));
		}
		const Constructor = WebSocket as unknown as WebSocketWithOptions;
		const socket = new Constructor(url.href, {
			headers: {
				authorization: authorization(endpoint.password),
				"x-kit-instance-id": endpoint.registration.instanceId,
			},
		});
		if (this.explicitClose || generation !== this.attemptGeneration) {
			socket.close(1000, "Connection attempt superseded");
			throw new Error("Local session connection attempt was cancelled");
		}
		this.socket = socket;
		socket.addEventListener("message", (event) => {
			if (this.socket !== socket || generation !== this.attemptGeneration)
				return;
			try {
				const record: unknown = JSON.parse(String(event.data));
				options.onRecord(record);
			} catch {
				this.fail(socket, new Error("Local session sent invalid JSON"));
			}
		});
		socket.addEventListener("close", (event) => {
			if (this.socket === socket) this.socket = null;
			if (
				!this.explicitClose &&
				generation === this.attemptGeneration &&
				this.callbacks === options
			) {
				options.onClose(
					new Error(
						`Local session connection closed (${event.code}${event.reason ? `: ${event.reason}` : ""})`,
					),
				);
			}
		});
		return new Promise<void>((resolve, reject) => {
			let settled = false;
			const cleanup = () => {
				socket.removeEventListener("open", opened);
				socket.removeEventListener("error", failed);
				socket.removeEventListener("close", closed);
			};
			const opened = () => {
				if (settled) return;
				settled = true;
				cleanup();
				resolve();
			};
			const failed = () => {
				if (settled) return;
				settled = true;
				cleanup();
				const error = new Error("Failed to connect to the local session");
				this.fail(socket, error);
				reject(error);
			};
			const closed = () => {
				if (settled) return;
				settled = true;
				cleanup();
				reject(new Error("Local session connection closed before opening"));
			};
			socket.addEventListener("open", opened);
			socket.addEventListener("error", failed);
			socket.addEventListener("close", closed);
		});
	}

	private fail(socket: WebSocket, error: Error): void {
		if (this.socket !== socket) return;
		this.socket = null;
		if (
			socket.readyState === WebSocket.OPEN ||
			socket.readyState === WebSocket.CONNECTING
		) {
			socket.close(1002, error.message);
		}
		if (!this.explicitClose) this.callbacks?.onClose(error);
	}
}
