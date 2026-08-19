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
import { startShutdownWatchdog } from "./shutdown-watchdog";
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
	publicUrl?: string;
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
	let forcedShutdownTimer: ReturnType<typeof setTimeout> | null = null;
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
			publicUrl: options.publicUrl,
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
		if (signalExitCode !== 0) {
			console.error("[kit] forcing web TUI shutdown after repeated signal");
			process.exit(signalExitCode);
		}
		signalExitCode = signal === "SIGINT" ? 130 : 143;
		// A broken native integration or app disposer must not make the process
		// permanently unkillable. Normal cleanup cancels this emergency fallback.
		forcedShutdownTimer = setTimeout(() => {
			console.error("[kit] forcing web TUI shutdown after cleanup timed out");
			process.exit(signalExitCode);
		}, 10_000);
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
			`kit web TUI mode (experimental) available at ${options.publicUrl ?? started.url}`,
		);
		if (options.publicUrl) {
			console.error(`Internal listener: ${started.url}`);
		}
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
		const shutdownErrors: unknown[] = [];
		try {
			bridge.shutdown();
		} catch (error) {
			shutdownErrors.push(error);
		}
		try {
			await appPromise;
		} catch (error) {
			shutdownErrors.push(error);
		}
		try {
			await server.stop();
		} catch (error) {
			shutdownErrors.push(error);
		}
		if (forcedShutdownTimer) clearTimeout(forcedShutdownTimer);
		// Keep intercepting repeated signals until every cleanup attempt finishes.
		process.off("SIGINT", handleSigint);
		process.off("SIGTERM", handleSigterm);
		if (shutdownErrors.length === 0) {
			if (process.env.KIT_DEBUG_SHUTDOWN) {
				console.error("[kit] web TUI shutdown complete");
			}
		} else {
			exitCode = 1;
			for (const error of shutdownErrors) {
				console.error(
					`[kit] web TUI shutdown failed: ${error instanceof Error ? error.message : String(error)}`,
				);
			}
		}
	}
	const finalExitCode = signalExitCode !== 0 ? signalExitCode : exitCode;
	process.exitCode = finalExitCode;
	// Bootstrap deliberately does not own process exit for custom terminals.
	// Arm the fallback only after renderer/session and server cleanup complete.
	startShutdownWatchdog(finalExitCode);
	return finalExitCode;
}
