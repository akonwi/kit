import { randomUUID } from "node:crypto";
import type { Server } from "bun";
import type { KitServer } from "./kit-server";
import {
	LOCAL_SERVER_PROTOCOL_VERSION,
	LOCAL_SERVER_USERNAME,
	type LocalServerPaths,
	removeLocalServerRegistration,
	writeLocalServerRegistration,
} from "./local-server-files";
import {
	LocalSessionRpcChannel,
	type LocalSessionSocketData,
	parseLocalSessionResumeCursor,
} from "./local-session-rpc";
import { WebAccessPolicy } from "./web-access-policy";

export type LocalKitServerOptions = {
	kitVersion: string;
	password: string;
	paths: LocalServerPaths;
};

export class LocalKitServer {
	private readonly instanceId = randomUUID();
	private readonly startedAt = new Date().toISOString();
	private readonly accessPolicy: WebAccessPolicy;
	private http: Server<LocalSessionSocketData> | null = null;
	private stopPromise: Promise<void> | null = null;
	private readonly sessionChannels = new Map<string, LocalSessionRpcChannel>();
	private readonly startingChannels = new Map<
		string,
		Promise<LocalSessionRpcChannel>
	>();
	private resolveStopped: (() => void) | null = null;
	private readonly stopped = new Promise<void>((resolve) => {
		this.resolveStopped = resolve;
	});

	constructor(
		private readonly server: KitServer,
		private readonly options: LocalKitServerOptions,
	) {
		this.accessPolicy = new WebAccessPolicy({
			hostname: "127.0.0.1",
			port: 0,
			allowOriginless: true,
			basicAuth: {
				username: LOCAL_SERVER_USERNAME,
				password: options.password,
			},
			authRealm: "Kit local server",
		});
	}

	async start(): Promise<{ url: string; instanceId: string }> {
		if (this.http) throw new Error("Local Kit server is already running");
		const http = Bun.serve<LocalSessionSocketData>({
			hostname: "127.0.0.1",
			port: 0,
			maxRequestBodySize: 64 * 1024,
			fetch: (request, bunServer) => this.handleRequest(request, bunServer),
			websocket: {
				maxPayloadLength: 1024 * 1024,
				backpressureLimit: 16 * 1024 * 1024,
				closeOnBackpressureLimit: true,
				open: (socket) => socket.data.channel.open(socket),
				message: (socket, message) =>
					socket.data.channel.message(socket, message),
				close: (socket) => socket.data.channel.close(socket),
			},
		});
		this.http = http;
		const port = http.port;
		if (port === undefined) {
			await this.stop();
			throw new Error("Local Kit server did not bind a port");
		}
		this.accessPolicy.setListenerAddress("127.0.0.1", port);
		const url = http.url.origin;
		try {
			await writeLocalServerRegistration(this.options.paths, {
				pid: process.pid,
				url,
				instanceId: this.instanceId,
				kitVersion: this.options.kitVersion,
				protocolVersion: LOCAL_SERVER_PROTOCOL_VERSION,
				startedAt: this.startedAt,
			});
		} catch (error) {
			await this.stop();
			throw error;
		}
		return { url, instanceId: this.instanceId };
	}

	waitForStop(): Promise<void> {
		return this.stopped;
	}

	stop(): Promise<void> {
		if (this.stopPromise) return this.stopPromise;
		this.stopPromise = this.performStop();
		return this.stopPromise;
	}

	private async performStop(): Promise<void> {
		const http = this.http;
		this.http = null;
		let cleanupError: unknown;
		try {
			await http?.stop(true);
		} catch (error) {
			cleanupError = error;
		}
		for (const channel of this.sessionChannels.values()) channel.dispose();
		this.sessionChannels.clear();
		try {
			await this.server.dispose();
		} catch (error) {
			cleanupError ??= error;
		}
		await removeLocalServerRegistration(
			this.options.paths,
			this.instanceId,
		).catch((error) => {
			cleanupError ??= error;
		});
		this.resolveStopped?.();
		this.resolveStopped = null;
		if (cleanupError) throw cleanupError;
	}

	private async handleRequest(
		request: Request,
		bunServer: Server<LocalSessionSocketData>,
	): Promise<Response | undefined> {
		const url = new URL(request.url);
		const rpcSessionId = this.rpcSessionId(url.pathname);
		const accessAllowed =
			rpcSessionId === null
				? this.accessPolicy.isAllowedHttpRequest(request, url)
				: this.accessPolicy.isAllowedWebSocketRequest(request, url);
		if (!accessAllowed) {
			return new Response("Host or origin not allowed", { status: 403 });
		}
		if (!this.accessPolicy.isAuthorized(request)) {
			return this.accessPolicy.authenticationRequiredResponse();
		}
		if (rpcSessionId !== null) {
			if (request.headers.get("x-kit-instance-id") !== this.instanceId) {
				return this.json({ error: "Local server instance changed" }, 409);
			}
			if (request.method !== "GET") {
				return new Response("Method not allowed", { status: 405 });
			}
			try {
				const channel = await this.getSessionChannel(rpcSessionId);
				if (
					bunServer.upgrade(request, {
						data: {
							channel,
							resume: parseLocalSessionResumeCursor(url),
							pendingCommands: 0,
						},
					})
				) {
					return undefined;
				}
				return new Response("WebSocket upgrade required", { status: 426 });
			} catch (error) {
				return this.json(
					{
						error: error instanceof Error ? error.message : String(error),
					},
					404,
				);
			}
		}
		if (request.method === "GET" && url.pathname === "/api/health") {
			return this.json({
				ok: true,
				pid: process.pid,
				instanceId: this.instanceId,
				kitVersion: this.options.kitVersion,
				protocolVersion: LOCAL_SERVER_PROTOCOL_VERSION,
				startedAt: this.startedAt,
			});
		}
		if (request.method === "GET" && url.pathname === "/api/sessions") {
			const cwd = url.searchParams.get("cwd") ?? undefined;
			return this.json({ sessions: await this.server.listSessions(cwd) });
		}
		if (request.method === "POST" && url.pathname === "/api/sessions") {
			const body = await this.requestBody(request);
			if (typeof body.cwd !== "string" || !body.cwd) {
				return this.json({ error: "cwd must be a non-empty string" }, 400);
			}
			return this.json(
				{ session: await this.server.createSession(body.cwd) },
				201,
			);
		}
		if (request.method === "POST" && url.pathname === "/api/shutdown") {
			if (request.headers.get("x-kit-instance-id") !== this.instanceId) {
				return this.json({ error: "Local server instance changed" }, 409);
			}
			setTimeout(() => {
				void this.stop().catch((error) => {
					console.error(
						`Local server shutdown failed: ${error instanceof Error ? error.message : String(error)}`,
					);
				});
			});
			return this.json({ stopping: true });
		}
		return new Response("Not found", { status: 404 });
	}

	private rpcSessionId(pathname: string): string | null {
		const match = pathname.match(/^\/api\/sessions\/([^/]+)\/rpc$/);
		if (!match?.[1]) return null;
		try {
			const sessionId = decodeURIComponent(match[1]);
			return sessionId.length <= 128 && /^[A-Za-z0-9._-]+$/.test(sessionId)
				? sessionId
				: null;
		} catch {
			return null;
		}
	}

	private getSessionChannel(
		sessionId: string,
	): Promise<LocalSessionRpcChannel> {
		const existing = this.sessionChannels.get(sessionId);
		if (existing) return Promise.resolve(existing);
		const pending = this.startingChannels.get(sessionId);
		if (pending) return pending;
		const starting = this.server
			.connectSession(sessionId)
			.then((connection) => {
				if (this.stopPromise) {
					connection.close();
					throw new Error("Local Kit server is shutting down");
				}
				let channel: LocalSessionRpcChannel;
				try {
					channel = new LocalSessionRpcChannel(connection);
				} catch (error) {
					connection.close();
					throw error;
				}
				this.sessionChannels.set(sessionId, channel);
				return channel;
			})
			.finally(() => this.startingChannels.delete(sessionId));
		this.startingChannels.set(sessionId, starting);
		return starting;
	}

	private async requestBody(
		request: Request,
	): Promise<Record<string, unknown>> {
		try {
			const value: unknown = await request.json();
			return typeof value === "object" &&
				value !== null &&
				!Array.isArray(value)
				? (value as Record<string, unknown>)
				: {};
		} catch {
			return {};
		}
	}

	private json(value: unknown, status = 200): Response {
		return Response.json(value, {
			status,
			headers: { "cache-control": "no-store" },
		});
	}
}
