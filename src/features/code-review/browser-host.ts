import { fileURLToPath } from "node:url";
import type {
	AgentRuntime,
	AgentRuntimeEvent,
} from "../../runtime/agent-runtime";
import { openExternal } from "../../shell/open-external";
import { theme } from "../../shell/theme";
import { loadReviewFiles } from "../review/model";

type CodeReviewSubmission = {
	submittedAt: string;
	files: Array<{
		path: string;
		fileComment: string;
		ranges: Array<{
			side: "additions" | "deletions";
			startLine: number;
			endLine: number;
			comment: string;
		}>;
	}>;
};

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

class CodeReviewBrowserHost {
	private server: Bun.Server<SocketData> | null = null;
	private readonly clients = new Set<Bun.ServerWebSocket<SocketData>>();
	private token = crypto.randomUUID();
	private activeRuntime: AgentRuntime | null = null;
	private unsubscribeRuntime: (() => void) | null = null;
	private state: CodeReviewBrowserState | null = null;
	private refreshCounter = 0;
	private appBundlePromise: Promise<string> | null = null;
	private onReviewSubmitted: ((review: CodeReviewSubmission) => void) | null =
		null;

	setOnReviewSubmitted(
		handler: ((review: CodeReviewSubmission) => void) | null,
	): void {
		this.onReviewSubmitted = handler;
	}

	clearPendingReview(): void {
		// Pending review attachment state currently lives outside the browser host.
	}

	async launch(runtime: AgentRuntime): Promise<void> {
		this.ensureServer();
		this.attachRuntime(runtime);
		await this.refreshState(runtime, "launch");

		const url = this.getSessionUrl();
		await openExternal(url);
		runtime.emitInfo("Code review opened", [url]);
	}

	private ensureServer(): void {
		if (this.server) return;

		this.server = Bun.serve<SocketData>({
			port: 0,
			fetch: async (request, server) => {
				const url = new URL(request.url);

				if (url.pathname === "/health") {
					return Response.json({ ok: true });
				}

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
					this.send(ws, {
						type: "connected",
						sessionUrl: this.getSessionUrl(),
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
					void this.handleClientMessage(ws, text);
				},
			},
		});
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
	<script type="module" src="/app.js?token=${this.token}"></script>
</body>
</html>`;
	}
}

export const codeReviewBrowserHost = new CodeReviewBrowserHost();
