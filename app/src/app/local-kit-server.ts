import { randomUUID } from "node:crypto";
import type { KitServer } from "./kit-server";
import {
	LOCAL_SERVER_PROTOCOL_VERSION,
	LOCAL_SERVER_USERNAME,
	type LocalServerPaths,
	removeLocalServerRegistration,
	writeLocalServerRegistration,
} from "./local-server-files";
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
	private http: ReturnType<typeof Bun.serve> | null = null;
	private stopPromise: Promise<void> | null = null;
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
		const http = Bun.serve({
			hostname: "127.0.0.1",
			port: 0,
			maxRequestBodySize: 64 * 1024,
			fetch: (request) => this.handleRequest(request),
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

	private async handleRequest(request: Request): Promise<Response> {
		const url = new URL(request.url);
		if (!this.accessPolicy.isAllowedHttpRequest(request, url)) {
			return new Response("Host or origin not allowed", { status: 403 });
		}
		if (!this.accessPolicy.isAuthorized(request)) {
			return this.accessPolicy.authenticationRequiredResponse();
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
