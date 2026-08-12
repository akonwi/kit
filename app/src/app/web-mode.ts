import { createHeadlessHost } from "./headless-host";
import { RemoteAttachmentStore } from "./remote-attachment-store";
import { RemoteInteractionBroker } from "./remote-interaction-broker";
import { resolveRpcSession, selectStartupModel } from "./rpc-mode";
import { RpcSessionHost } from "./rpc-session-host";
import { type WebBasicAuthCredentials, WebRpcServer } from "./web-rpc-server";

export type WebModeOptions = {
	allowedHosts?: string[];
	basicAuth?: WebBasicAuthCredentials;
	allowedOrigins?: string[];
	hostname?: string;
	port?: number;
	model?: string;
	sessionId?: string;
};

export async function runWebMode(
	cwd: string,
	options: WebModeOptions = {},
): Promise<number> {
	let headlessHost: Awaited<ReturnType<typeof createHeadlessHost>> | null =
		null;
	let interactions: RemoteInteractionBroker | null = null;
	let attachments: RemoteAttachmentStore | null = null;
	let rpcHost: RpcSessionHost | null = null;
	let webServer: WebRpcServer | null = null;
	let signalExitCode = 0;
	let stop: (() => void) | undefined;
	const stopped = new Promise<void>((resolve) => {
		stop = resolve;
	});
	const handleSignal = (signal: "SIGINT" | "SIGTERM") => {
		const exitCode = signal === "SIGINT" ? 130 : 143;
		if (signalExitCode !== 0) process.exit(exitCode);
		signalExitCode = exitCode;
		stop?.();
	};
	const handleSigint = () => handleSignal("SIGINT");
	const handleSigterm = () => handleSignal("SIGTERM");
	process.on("SIGINT", handleSigint);
	process.on("SIGTERM", handleSigterm);

	let exitCode = 0;
	try {
		const resolved = await resolveRpcSession(cwd, {
			noSession: false,
			sessionId: options.sessionId,
		});
		interactions = new RemoteInteractionBroker();
		attachments = new RemoteAttachmentStore();
		headlessHost = await createHeadlessHost(resolved.session, {
			persistSession: true,
			interactions,
			externalPlugins: true,
			remoteChrome: true,
			remotePromptCommands: true,
		});
		if (options.model) {
			const model = selectStartupModel(
				headlessHost.runtime.getAvailableModels(),
				options.model,
			);
			headlessHost.runtime.setModel(model);
			await headlessHost.runtime.waitForModelAdaptation();
		}

		rpcHost = new RpcSessionHost(headlessHost.runtime, {
			persistSessions: true,
			interactions,
			attachments,
			scratchpad: headlessHost.scratchpad ?? undefined,
			commands: headlessHost.commands,
			header: headlessHost.header,
			footer: headlessHost.footer,
			waitForWorkspaceReady: headlessHost.waitForWorkspaceReady,
			reloadHost: headlessHost.reload,
			allowLegacySessionPaths: false,
		});
		if (
			!options.basicAuth &&
			(options.allowedHosts?.includes("*") === true ||
				options.allowedOrigins?.includes("*") === true)
		) {
			console.warn(
				"Warning: wildcard web access is enabled without --auth; rely on a trusted network or access-control proxy.",
			);
		}
		webServer = new WebRpcServer(rpcHost, {
			hostname: options.hostname,
			port: options.port,
			allowedHosts: options.allowedHosts,
			allowedOrigins: options.allowedOrigins,
			basicAuth: options.basicAuth,
			attachments,
		});
		const address = webServer.start();
		console.log(`Kit web mode listening at ${address.url}`);
		await stopped;
		exitCode = signalExitCode;
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		exitCode = 1;
	} finally {
		process.off("SIGINT", handleSigint);
		process.off("SIGTERM", handleSigterm);
		const cleanupErrors: unknown[] = [];
		try {
			await webServer?.stop();
		} catch (error) {
			cleanupErrors.push(error);
		}
		try {
			await rpcHost?.abortAndWait();
		} catch (error) {
			cleanupErrors.push(error);
		} finally {
			rpcHost?.dispose();
			interactions?.dispose();
			attachments?.dispose();
		}
		try {
			await headlessHost?.dispose();
		} catch (error) {
			cleanupErrors.push(error);
		}
		for (const error of cleanupErrors) {
			console.error(
				`Web mode cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
			);
		}
		if (cleanupErrors.length > 0 && signalExitCode === 0) exitCode = 1;
	}
	return exitCode;
}
