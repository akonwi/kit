import { spawn } from "node:child_process";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test as base, type Page } from "@playwright/test";

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const binary = path.join(appDir, "dist", "kit");

const fidelityTheme = {
	tokens: {
		bg: "#fdf6e3",
		bgSurface: "#eee8d5",
		bgMuted: "#ddd6c1",
		bgAccent: "#c8bea4",
		borderDefault: "#b8ad91",
		textPrimary: "#123456",
		textSecondary: "#586e75",
		cursor: "#d33682",
	},
};

type ExitResult = { code: number | null; signal: NodeJS.Signals | null };

type WebTuiServer = {
	url: string;
	port: number;
	allowExitCode(code: number): void;
};

type BrowserDiagnostics = {
	consoleErrors: string[];
	pageErrors: string[];
	failedRequests: string[];
};

type WebTuiFixtures = {
	webTuiServer: WebTuiServer;
	webTuiPage: {
		diagnostics: BrowserDiagnostics;
		page: Page;
		server: WebTuiServer;
		url: string;
	};
};

async function availablePort(): Promise<number> {
	return new Promise((resolve, reject) => {
		const server = createServer();
		server.once("error", reject);
		server.listen(0, "127.0.0.1", () => {
			const address = server.address();
			if (!address || typeof address === "string") {
				server.close();
				reject(new Error("Failed to allocate a browser-TUI test port"));
				return;
			}
			server.close((error) => {
				if (error) reject(error);
				else resolve(address.port);
			});
		});
	});
}

async function waitForHealth(
	url: string,
	exited: Promise<ExitResult>,
): Promise<void> {
	const deadline = Date.now() + 30_000;
	while (Date.now() < deadline) {
		const result = await Promise.race([
			exited.then((exit) => ({ type: "exit" as const, exit })),
			fetch(`${url}/api/health`)
				.then((response) => ({ type: "response" as const, response }))
				.catch(() => ({ type: "retry" as const })),
		]);
		if (result.type === "exit") {
			throw new Error(
				`Kit exited before becoming healthy (code=${result.exit.code}, signal=${result.exit.signal})`,
			);
		}
		if (result.type === "response" && result.response.ok) return;
		await new Promise((resolve) => setTimeout(resolve, 100));
	}
	throw new Error("Timed out waiting for Kit browser-TUI health endpoint");
}

export const test = base.extend<WebTuiFixtures>({
	// biome-ignore lint/correctness/noEmptyPattern: Playwright fixture signatures require object destructuring.
	webTuiServer: async ({}, use, testInfo) => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-web-tui-e2e-"));
		const allowedExitCodes = new Set([143]);
		let child: ReturnType<typeof spawn> | null = null;
		let exited: Promise<ExitResult> | null = null;
		let stdout = "";
		let stderr = "";
		let primaryError: unknown;
		try {
			const home = path.join(root, "home");
			const workspace = path.join(root, "workspace");
			const themeDir = path.join(home, ".kit", "themes");
			await mkdir(themeDir, { recursive: true });
			await mkdir(workspace, { recursive: true });
			await writeFile(
				path.join(home, ".kit", "settings.json"),
				JSON.stringify({ theme: "fidelity-light" }),
			);
			await writeFile(
				path.join(themeDir, "fidelity-light.json"),
				JSON.stringify(fidelityTheme),
			);

			const port = await availablePort();
			const url = `http://127.0.0.1:${port}`;
			child = spawn(
				binary,
				[
					"--web-tui",
					"--no-session",
					"--port",
					String(port),
					"--public-url",
					url,
				],
				{
					cwd: workspace,
					env: {
						...process.env,
						HOME: home,
						USERPROFILE: home,
						KIT_DEBUG_SHUTDOWN: "1",
					},
					stdio: ["ignore", "pipe", "pipe"],
				},
			);
			child.stdout?.on("data", (chunk: Buffer) => {
				stdout = (stdout + chunk.toString("utf8")).slice(-64 * 1024);
			});
			child.stderr?.on("data", (chunk: Buffer) => {
				stderr = (stderr + chunk.toString("utf8")).slice(-64 * 1024);
			});
			exited = new Promise<ExitResult>((resolve) => {
				child?.once("exit", (code, signal) => resolve({ code, signal }));
			});
			await waitForHealth(url, exited);
			await use({
				url,
				port,
				allowExitCode: (code) => allowedExitCodes.add(code),
			});
		} catch (error) {
			primaryError = error;
		}

		const cleanupErrors: unknown[] = [];
		if (child && exited) {
			if (child.exitCode === null && child.signalCode === null) {
				child.kill("SIGTERM");
			}
			let exit = await Promise.race([
				exited,
				new Promise<"timeout">((resolve) =>
					setTimeout(() => resolve("timeout"), 15_000),
				),
			]);
			if (exit === "timeout") {
				child.kill("SIGKILL");
				exit = await exited;
			}
			if (exit.code === null || !allowedExitCodes.has(exit.code)) {
				cleanupErrors.push(
					new Error(
						`Kit browser-TUI teardown exited with code ${exit.code} and signal ${exit.signal}; expected ${[...allowedExitCodes].join(" or ")}\n${stderr}`,
					),
				);
			} else if (!stderr.includes("[kit] web TUI shutdown complete")) {
				cleanupErrors.push(
					new Error(`Kit did not report completed shutdown\n${stderr}`),
				);
			}
		}
		try {
			await testInfo.attach("kit-stdout", {
				body: Buffer.from(stdout),
				contentType: "text/plain",
			});
			await testInfo.attach("kit-stderr", {
				body: Buffer.from(stderr),
				contentType: "text/plain",
			});
		} catch (error) {
			cleanupErrors.push(error);
		}
		try {
			await rm(root, { recursive: true, force: true });
		} catch (error) {
			cleanupErrors.push(error);
		}
		const errors = [
			...(primaryError === undefined ? [] : [primaryError]),
			...cleanupErrors,
		];
		if (errors.length > 1) {
			throw new AggregateError(errors, "Browser-TUI test or cleanup failed");
		}
		if (errors.length === 1) {
			const error = errors[0];
			throw error instanceof Error ? error : new Error(String(error));
		}
	},

	webTuiPage: async ({ browser, webTuiServer }, use) => {
		const context = await browser.newContext({
			viewport: { width: 1_000, height: 700 },
			deviceScaleFactor: 1,
		});
		const page = await context.newPage();
		const diagnostics: BrowserDiagnostics = {
			consoleErrors: [],
			pageErrors: [],
			failedRequests: [],
		};
		page.on("console", (message) => {
			if (message.type() === "error") {
				diagnostics.consoleErrors.push(message.text());
			}
		});
		page.on("pageerror", (error) => diagnostics.pageErrors.push(error.message));
		page.on("requestfailed", (request) =>
			diagnostics.failedRequests.push(request.url()),
		);
		try {
			await use({
				diagnostics,
				page,
				server: webTuiServer,
				url: webTuiServer.url,
			});
		} finally {
			await context.close();
		}
	},
});

export { expect } from "@playwright/test";
