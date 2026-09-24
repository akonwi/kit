import { createServer } from "node:http";
import {
	MCP_OAUTH_CALLBACK_PATH,
	MCP_OAUTH_CALLBACK_PORT,
} from "./oauth-provider";

export type McpOAuthCallbackServer = {
	waitForCode: () => Promise<string>;
	close: () => Promise<void>;
};

export async function startMcpOAuthCallbackServer(): Promise<McpOAuthCallbackServer> {
	let resolveCode: ((code: string) => void) | null = null;
	let rejectCode: ((error: Error) => void) | null = null;
	const codePromise = new Promise<string>((resolve, reject) => {
		resolveCode = resolve;
		rejectCode = reject;
	});

	const server = createServer((req, res) => {
		const requestUrl = new URL(
			req.url ?? "/",
			`http://127.0.0.1:${MCP_OAUTH_CALLBACK_PORT}`,
		);
		if (requestUrl.pathname !== MCP_OAUTH_CALLBACK_PATH) {
			res.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
			res.end("Not found");
			return;
		}

		const code = requestUrl.searchParams.get("code");
		const error = requestUrl.searchParams.get("error");
		if (code) {
			res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
			res.end(
				"<html><body><h1>Kit MCP login complete</h1><p>You can close this window and return to Kit.</p><script>setTimeout(() => window.close(), 750);</script></body></html>",
			);
			resolveCode?.(code);
			return;
		}

		const message = error
			? `OAuth authorization failed: ${error}`
			: "OAuth authorization failed: missing code";
		res.writeHead(400, { "Content-Type": "text/html; charset=utf-8" });
		res.end(
			`<html><body><h1>Kit MCP login failed</h1><p>${message}</p></body></html>`,
		);
		rejectCode?.(new Error(message));
	});

	await new Promise<void>((resolve, reject) => {
		server.once("error", reject);
		server.listen(MCP_OAUTH_CALLBACK_PORT, "127.0.0.1", () => {
			server.off("error", reject);
			resolve();
		});
	});

	return {
		waitForCode: () => codePromise,
		close: async () => {
			await new Promise<void>((resolve, reject) => {
				server.close((error) => {
					if (error) {
						reject(error);
						return;
					}
					resolve();
				});
			});
		},
	};
}
