import { fileURLToPath } from "node:url";
import type {
	AgentRuntime,
	AgentRuntimeEvent,
} from "../../runtime/agent-runtime";
import { openExternal } from "../../shell/open-external";
import { theme } from "../../shell/theme";
import { loadReviewFiles } from "../review/model";
import type { CodeReviewSubmission } from "./attachment";
import type { CodeReviewHostStatus } from "./state";

type CodeReviewClientMessage =
	| { type: "ready" }
	| { type: "request_state" }
	| { type: "refresh_diff" }
	| { type: "submit_review_state"; review: CodeReviewSubmission };

type CodeReviewServerMessage =
	| { type: "connected"; sessionUrl: string }
	| { type: "state"; state: CodeReviewBrowserState; reason: string }
	| {
			type: "submission_saved";
			submittedAt: string;
			fileCount: number;
			commentCount: number;
	  };

type CodeReviewBrowserHunk = {
	id: string;
	header: string;
	context: string;
	changeCount: number;
	additionStart: number;
	additionCount: number;
	deletionStart: number;
	deletionCount: number;
};

type CodeReviewBrowserFile = {
	id: string;
	noteKey: string;
	path: string;
	prevPath?: string;
	status: string;
	filetype?: string;
	changeCount: number;
	hunkCount: number;
	rawPatch: string;
	hunks: CodeReviewBrowserHunk[];
};

type CodeReviewBrowserState = {
	sessionId: string;
	sessionName: string | null;
	cwd: string;
	model: string | null;
	lastUpdatedAt: string;
	review: {
		files: CodeReviewBrowserFile[];
		totalFileCount: number;
		totalHunkCount: number;
		totalChangeCount: number;
		error: string | null;
	};
};

type SocketData = {
	connectedAt: string;
};

type BrowserTheme = {
	bg: string;
	bgSurface: string;
	bgMuted: string;
	bgAccent: string;
	borderDefault: string;
	borderAccent: string;
	textPrimary: string;
	textSecondary: string;
	textMuted: string;
	userText: string;
	toolText: string;
	warningText: string;
	errorText: string;
};

function getBrowserTheme(): BrowserTheme {
	return {
		bg: theme.bg,
		bgSurface: theme.bgSurface,
		bgMuted: theme.bgMuted,
		bgAccent: theme.bgAccent,
		borderDefault: theme.borderDefault,
		borderAccent: theme.borderAccent,
		textPrimary: theme.textPrimary,
		textSecondary: theme.textSecondary,
		textMuted: theme.textMuted,
		userText: theme.userText,
		toolText: theme.toolText,
		warningText: theme.warningText,
		errorText: theme.errorText,
	};
}

const SERVER_PORT_BASE = 41000;
const SERVER_PORT_RANGE = 2000;
const SERVER_PORT_ATTEMPTS = 12;
const SERVER_READY_RETRY_DELAYS_MS = [25, 75, 150, 300, 600, 1000];
const INITIAL_HOST_STATUS: CodeReviewHostStatus = {
	serverState: "idle",
	port: null,
	clientConnected: false,
	launchInFlight: false,
	lastError: null,
};

class CodeReviewBrowserHost {
	private server: Bun.Server<SocketData> | null = null;
	private readonly clients = new Set<Bun.ServerWebSocket<SocketData>>();
	private activeRuntime: AgentRuntime | null = null;
	private activeSessionId: string | null = null;
	private unsubscribeRuntime: (() => void) | null = null;
	private state: CodeReviewBrowserState | null = null;
	private refreshCounter = 0;
	private appBundlePromise: Promise<string> | null = null;
	private onReviewSubmitted: ((review: CodeReviewSubmission) => void) | null =
		null;
	private status: CodeReviewHostStatus = INITIAL_HOST_STATUS;
	private readonly statusListeners = new Set<
		(status: CodeReviewHostStatus) => void
	>();

	setOnReviewSubmitted(
		handler: ((review: CodeReviewSubmission) => void) | null,
	): void {
		this.onReviewSubmitted = handler;
	}

	clearPendingReview(): void {
		// Pending review attachment state currently lives outside the browser host.
	}

	getStatus(): CodeReviewHostStatus {
		return { ...this.status };
	}

	subscribeStatus(
		listener: (status: CodeReviewHostStatus) => void,
	): () => void {
		this.statusListeners.add(listener);
		listener(this.getStatus());
		return () => {
			this.statusListeners.delete(listener);
		};
	}

	dispose(): void {
		this.unsubscribeRuntime?.();
		this.unsubscribeRuntime = null;
		this.activeRuntime = null;
		this.server?.stop(true);
		this.server = null;
		this.clients.clear();
		this.activeSessionId = null;
		this.state = null;
		this.setStatus(INITIAL_HOST_STATUS);
	}

	async activate(runtime: AgentRuntime): Promise<void> {
		await this.ensureServer(runtime);
		this.attachRuntime(runtime);
		await this.refreshState(runtime, "activate");
	}

	async launch(runtime: AgentRuntime): Promise<void> {
		this.setStatus({
			serverState: this.server ? this.status.serverState : "starting",
			port: this.server?.port ?? null,
			clientConnected: this.clients.size > 0,
			launchInFlight: true,
			lastError: null,
		});
		try {
			await this.activate(runtime);
			await this.waitForServerReady();

			const url = this.getSessionUrl();
			await openExternal(url);
			runtime.emitInfo("Code review opened", [url]);
			this.setStatus({ launchInFlight: false, lastError: null });
		} catch (error) {
			this.setStatus({
				serverState: "error",
				launchInFlight: false,
				lastError: String(error),
			});
			throw error;
		}
	}

	private async ensureServer(runtime: AgentRuntime): Promise<void> {
		const sessionId = runtime.getSession().id;
		this.setStatus({
			serverState: "starting",
			launchInFlight: this.status.launchInFlight,
			clientConnected: this.clients.size > 0,
			lastError: null,
		});
		if (this.server && this.activeSessionId === sessionId) {
			return;
		}
		if (this.server) {
			this.server.stop(true);
			this.server = null;
			this.clients.clear();
			this.setStatus({ clientConnected: false, port: null });
		}
		this.activeSessionId = sessionId;

		const preferredPort = this.getSessionPort(sessionId);
		let lastError: unknown = null;
		for (let offset = 0; offset < SERVER_PORT_ATTEMPTS; offset++) {
			const port =
				SERVER_PORT_BASE +
				((preferredPort - SERVER_PORT_BASE + offset) % SERVER_PORT_RANGE);
			try {
				this.server = Bun.serve<SocketData>({
					port,
					fetch: async (request, server) => {
						const url = new URL(request.url);

						if (!this.matchesSession(url)) {
							return new Response("Session not found", { status: 404 });
						}

						if (url.pathname === "/health") {
							return Response.json({
								ok: true,
								sessionId: this.activeSessionId,
								hasState: this.state !== null,
								port: server.port,
							});
						}

						if (url.pathname === "/state") {
							if (!this.state) {
								return Response.json(
									{ error: "State not ready" },
									{ status: 503 },
								);
							}
							return Response.json(this.state, {
								headers: { "cache-control": "no-store" },
							});
						}

						if (url.pathname === "/ws") {
							const upgraded = server.upgrade(request, {
								data: { connectedAt: new Date().toISOString() },
							});
							if (upgraded) return undefined;
							return new Response("WebSocket upgrade failed", { status: 500 });
						}

						if (url.pathname === "/app.js") {
							try {
								return new Response(await this.getAppBundle(), {
									headers: {
										"content-type": "text/javascript; charset=utf-8",
										"cache-control": "no-store",
									},
								});
							} catch (error) {
								return new Response(String(error), { status: 500 });
							}
						}

						if (url.pathname === "/") {
							return new Response(this.renderHtml(), {
								headers: {
									"content-type": "text/html; charset=utf-8",
									"cache-control": "no-store",
								},
							});
						}

						return new Response("Not found", { status: 404 });
					},
					websocket: {
						open: (ws) => {
							this.clients.add(ws);
							this.setStatus({ clientConnected: true });
							this.send(ws, {
								type: "connected",
								sessionUrl: this.getSessionUrl(),
							});
							this.sendState(ws, "socket_open");
						},
						close: (ws) => {
							this.clients.delete(ws);
							this.setStatus({ clientConnected: this.clients.size > 0 });
						},
						message: (ws, message) => {
							const text =
								typeof message === "string"
									? message
									: Buffer.from(message).toString("utf8");
							void this.handleClientMessage(ws, text);
						},
					},
				});
				this.setStatus({
					serverState: "ready",
					port,
					clientConnected: this.clients.size > 0,
					lastError: null,
				});
				return;
			} catch (error) {
				lastError = error;
				if (!isAddressInUse(error)) {
					const message = String(error);
					this.setStatus({
						serverState: "error",
						lastError: message,
						port: null,
						clientConnected: false,
					});
					throw error;
				}
			}
		}

		const message = `Unable to start code review browser server after ${SERVER_PORT_ATTEMPTS} port attempts: ${String(lastError)}`;
		this.setStatus({
			serverState: "error",
			port: null,
			clientConnected: false,
			lastError: message,
		});
		throw new Error(message);
	}

	private async waitForServerReady(): Promise<void> {
		const healthUrl = this.getHealthUrl();
		let lastError: unknown = null;
		for (const delayMs of SERVER_READY_RETRY_DELAYS_MS) {
			try {
				const response = await fetch(healthUrl, { cache: "no-store" });
				if (response.ok) return;
				lastError = new Error(`Health check failed with ${response.status}`);
			} catch (error) {
				lastError = error;
			}
			await sleep(delayMs);
		}
		const message = `Code review browser server did not become ready: ${String(lastError)}`;
		this.setStatus({ serverState: "error", lastError: message });
		throw new Error(message);
	}

	private attachRuntime(runtime: AgentRuntime): void {
		if (this.activeRuntime === runtime && this.unsubscribeRuntime) return;

		this.unsubscribeRuntime?.();
		this.activeRuntime = runtime;
		this.unsubscribeRuntime = runtime.subscribe((event) => {
			void this.handleRuntimeEvent(runtime, event);
		});
	}

	private async handleRuntimeEvent(
		runtime: AgentRuntime,
		event: AgentRuntimeEvent,
	): Promise<void> {
		if (this.activeRuntime !== runtime) return;

		if (
			event.type === "session_changed" ||
			event.type === "session_updated" ||
			event.type === "turn_complete"
		) {
			await this.refreshState(runtime, event.type);
		}
	}

	private async handleClientMessage(
		ws: Bun.ServerWebSocket<SocketData>,
		payload: string,
	): Promise<void> {
		const runtime = this.activeRuntime;
		if (!runtime) return;

		let message: CodeReviewClientMessage;
		try {
			message = JSON.parse(payload) as CodeReviewClientMessage;
		} catch {
			return;
		}

		switch (message.type) {
			case "ready":
				this.sendState(ws, "browser_ready");
				break;
			case "request_state":
				await this.refreshState(runtime, "browser_request");
				break;
			case "refresh_diff":
				await this.refreshState(runtime, "browser_refresh_diff");
				break;
			case "submit_review_state": {
				const fileCount = message.review.files.length;
				const commentCount = message.review.files.reduce(
					(sum, file) =>
						sum +
						(file.fileComment.trim().length > 0 ? 1 : 0) +
						file.ranges.length,
					0,
				);
				this.onReviewSubmitted?.(message.review);
				this.send(ws, {
					type: "submission_saved",
					submittedAt: message.review.submittedAt,
					fileCount,
					commentCount,
				});
				break;
			}
		}
	}

	private async refreshState(
		runtime: AgentRuntime,
		reason: string,
	): Promise<void> {
		const refreshId = ++this.refreshCounter;
		this.state = await this.buildState(runtime);
		if (refreshId !== this.refreshCounter) return;
		this.broadcastState(reason);
	}

	private async buildState(
		runtime: AgentRuntime,
	): Promise<CodeReviewBrowserState> {
		const session = runtime.getSession();
		let reviewFiles: CodeReviewBrowserFile[] = [];
		let reviewError: string | null = null;

		try {
			reviewFiles = (await loadReviewFiles(session.cwd)).map((file) => ({
				id: file.id,
				noteKey: file.noteKey,
				path: file.path,
				prevPath: file.prevPath,
				status: file.status,
				filetype: file.filetype,
				changeCount: file.changeCount,
				hunkCount: file.hunks.length,
				rawPatch: file.rawPatch,
				hunks: file.hunks.map((hunk) => ({
					id: hunk.id,
					header: hunk.header,
					context: hunk.context,
					changeCount: hunk.changeCount,
					additionStart: hunk.additionStart,
					additionCount: hunk.additionCount,
					deletionStart: hunk.deletionStart,
					deletionCount: hunk.deletionCount,
				})),
			}));
		} catch (error) {
			reviewError = String(error);
		}

		const totalHunkCount = reviewFiles.reduce(
			(sum, file) => sum + file.hunkCount,
			0,
		);
		const totalChangeCount = reviewFiles.reduce(
			(sum, file) => sum + file.changeCount,
			0,
		);

		return {
			sessionId: session.id,
			sessionName: session.name ?? null,
			cwd: session.cwd,
			model: session.model ?? null,
			lastUpdatedAt: new Date().toISOString(),
			review: {
				files: reviewFiles,
				totalFileCount: reviewFiles.length,
				totalHunkCount,
				totalChangeCount,
				error: reviewError,
			},
		};
	}

	private async getAppBundle(): Promise<string> {
		if (!this.appBundlePromise) {
			this.appBundlePromise = this.buildAppBundle().catch((error) => {
				this.appBundlePromise = null;
				throw error;
			});
		}
		return this.appBundlePromise;
	}

	private async buildAppBundle(): Promise<string> {
		const entrypoint = fileURLToPath(new URL("./web/main.ts", import.meta.url));
		const buildConfig = {
			entrypoints: [entrypoint],
			format: "esm",
			target: "browser",
			minify: false,
			splitting: false,
			write: false,
		} as unknown as Bun.BuildConfig;
		const result = await Bun.build(buildConfig);

		if (!result.success || result.outputs.length === 0) {
			const logs = result.logs.map((log) => log.message).join("\n");
			throw new Error(logs || "Failed to build code review SPA bundle.");
		}

		const output = result.outputs[0];
		return await output.text();
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

	private setStatus(next: Partial<CodeReviewHostStatus>): void {
		this.status = {
			...this.status,
			...next,
		};
		for (const listener of this.statusListeners) {
			listener(this.getStatus());
		}
	}

	private getSessionUrl(): string {
		const server = this.server;
		if (!server) {
			throw new Error("Code review browser server has not started.");
		}
		const sessionId = this.activeSessionId;
		if (!sessionId) {
			throw new Error("Code review browser session is not active.");
		}
		return `http://127.0.0.1:${server.port}/?sessionId=${encodeURIComponent(sessionId)}`;
	}

	private getHealthUrl(): string {
		const server = this.server;
		if (!server) {
			throw new Error("Code review browser server has not started.");
		}
		const sessionId = this.activeSessionId;
		if (!sessionId) {
			throw new Error("Code review browser session is not active.");
		}
		return `http://127.0.0.1:${server.port}/health?sessionId=${encodeURIComponent(sessionId)}`;
	}

	private getSessionPort(sessionId: string): number {
		let hash = 0;
		for (let i = 0; i < sessionId.length; i++) {
			hash = (hash * 31 + sessionId.charCodeAt(i)) >>> 0;
		}
		return SERVER_PORT_BASE + (hash % SERVER_PORT_RANGE);
	}

	private matchesSession(url: URL): boolean {
		return url.searchParams.get("sessionId") === this.activeSessionId;
	}

	private renderHtml(): string {
		const bootstrap = JSON.stringify({ theme: getBrowserTheme() });
		return `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<meta name="color-scheme" content="dark" />
	<title>Kit Code Review</title>
</head>
<body>
	<div id="app"></div>
	<script>
		window.__KIT_CODE_REVIEW_BOOTSTRAP__ = ${bootstrap};
	</script>
	<script type="module" src="/app.js?sessionId=${this.activeSessionId}"></script>
</body>
</html>`;
	}
}

function isAddressInUse(error: unknown): boolean {
	return (
		error instanceof Error &&
		(typeof (error as NodeJS.ErrnoException).code === "string"
			? (error as NodeJS.ErrnoException).code === "EADDRINUSE"
			: error.message.includes("EADDRINUSE") ||
				error.message.toLowerCase().includes("address already in use"))
	);
}

function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

export const codeReviewBrowserHost = new CodeReviewBrowserHost();
