/**
 * Experimental `kit --web --experimental-tui` mode (ghostty-web experiment).
 *
 * Hosts Kit's real OpenTUI application in-process against virtual terminal
 * streams and exposes it to a browser terminal (ghostty-web + Ghostty WASM) over a
 * WebSocket. Unlike the semantic web mode this process runs exactly one
 * authoritative runtime: the OpenTUI App itself. No headless host is created,
 * so the persisted session has a single owner.
 *
 * The application boots lazily on the first client `init` so the browser-side
 * Ghostty core is connected while OpenTUI probes terminal capabilities and
 * queries the palette.
 */

import { subscribeThemeConfig } from "../shell/theme";
import type { ThemeConfig } from "../shell/themes/types";
import type { BrowserTheme } from "../web-tui/browser-theme";
import { WebTuiBridge } from "./web-tui-bridge";
import {
	type WebTuiBasicAuthCredentials,
	WebTuiServer,
} from "./web-tui-server";

function browserThemeFromConfig(config: ThemeConfig): BrowserTheme {
	return {
		background: config.tokens.bg,
		foreground: config.tokens.textPrimary,
		cursor: config.tokens.cursor,
		selectionBackground: config.tokens.bgAccent,
		statusBackground: config.tokens.bgSurface,
		statusForeground: config.tokens.textSecondary,
		statusBorder: config.tokens.borderDefault,
	};
}

export type WebTuiModeOptions = {
	allowedHosts?: string[];
	allowedOrigins?: string[];
	basicAuth?: WebTuiBasicAuthCredentials;
	hostname?: string;
	port?: number;
	newSession?: boolean;
	noSession?: boolean;
	sessionId?: string;
};

export async function runWebTuiMode(
	options: WebTuiModeOptions = {},
): Promise<number> {
	const bridge = new WebTuiBridge();
	let appPromise: Promise<void> | null = null;
	let appFailed = false;
	let signalExitCode = 0;
	let stop: (() => void) | undefined;
	const stopped = new Promise<void>((resolve) => {
		stop = resolve;
	});

	const startApp = () => {
		if (appPromise) return;
		appPromise = import("./bootstrap")
			.then(({ bootstrap }) =>
				bootstrap({
					newSession: options.newSession,
					noSession: options.noSession,
					sessionId: options.sessionId,
					terminal: bridge.terminal,
				}),
			)
			.catch((error) => {
				appFailed = true;
				console.error(
					`[kit] web TUI application failed: ${error instanceof Error ? error.message : String(error)}`,
				);
			})
			.finally(() => {
				stop?.();
			});
	};

	const server = new WebTuiServer(
		{
			attach: (client, cols, rows) => {
				bridge.attach(client, cols, rows);
				startApp();
			},
			detach: (client) => bridge.detach(client),
			input: (bytes) => bridge.input(bytes),
			resize: (cols, rows) => bridge.resize(cols, rows),
		},
		{
			hostname: options.hostname,
			port: options.port,
			allowedHosts: options.allowedHosts,
			allowedOrigins: options.allowedOrigins,
			basicAuth: options.basicAuth,
		},
	);

	const unsubscribeTheme = subscribeThemeConfig(
		(config) => {
			server.setBrowserTheme(browserThemeFromConfig(config));
		},
		{ emitCurrent: false },
	);

	const handleSignal = (signal: "SIGINT" | "SIGTERM") => {
		const exitCode = signal === "SIGINT" ? 130 : 143;
		if (signalExitCode !== 0) process.exit(exitCode);
		signalExitCode = exitCode;
		// Ask the hosted app to shut down cleanly. If bootstrap is still
		// creating the renderer, the bridge remembers the request and destroys
		// it from onRendererReady; appPromise then resolves `stopped`.
		if (!bridge.shutdown() && !appPromise) stop?.();
	};
	const handleSigint = () => handleSignal("SIGINT");
	const handleSigterm = () => handleSignal("SIGTERM");
	process.on("SIGINT", handleSigint);
	process.on("SIGTERM", handleSigterm);

	let exitCode = 0;
	try {
		if (
			!options.basicAuth &&
			(options.allowedHosts?.includes("*") === true ||
				options.allowedOrigins?.includes("*") === true)
		) {
			console.warn(
				"Warning: wildcard web access is enabled without --auth; rely on a trusted network or access-control proxy.",
			);
		}
		const started = server.start();
		console.error(
			`kit web TUI mode (experimental) listening on ${started.url}`,
		);
		console.error(
			"The OpenTUI application starts when the first browser client connects.",
		);
		await stopped;
		if (appFailed) exitCode = 1;
	} catch (error) {
		console.error(
			`kit --web --experimental-tui failed: ${error instanceof Error ? error.message : String(error)}`,
		);
		exitCode = 1;
	} finally {
		unsubscribeTheme();
		process.off("SIGINT", handleSigint);
		process.off("SIGTERM", handleSigterm);
		bridge.shutdown();
		await appPromise;
		await server.stop();
	}
	return signalExitCode !== 0 ? signalExitCode : exitCode;
}
