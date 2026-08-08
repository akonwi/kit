import type { Readable } from "node:stream";
import type { CommandRegistry } from "../features/commands";
import type { Api, Model } from "../runtime/agent";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { createHeadlessHost, takeOverStdout } from "./headless-host";
import { applyStartupModel } from "./headless-model";
import { resolveHeadlessSession } from "./headless-session";
import {
	type RpcCommand,
	RpcSessionHost,
	type RpcWriter,
} from "./rpc-session-host";

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** JSONL transport adapter for the shared RPC session host. */
export class RpcModeServer {
	private writeQueue = Promise.resolve();
	private readonly host: RpcSessionHost;
	private readonly unsubscribeHost: () => void;

	constructor(
		runtime: AgentRuntime,
		private readonly input: Readable,
		private readonly writeRecord: RpcWriter,
		persistSessions = false,
		commands?: CommandRegistry,
		waitForWorkspaceReady?: () => Promise<void>,
	) {
		this.host = new RpcSessionHost(runtime, {
			persistSessions,
			commands,
			waitForWorkspaceReady,
			allowLegacySessionPaths: true,
		});
		this.unsubscribeHost = this.host.subscribe((record) => {
			void this.write(record);
		});
	}

	async start(): Promise<void> {
		let buffer = Buffer.alloc(0);
		for await (const chunk of this.input) {
			const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
			buffer = Buffer.concat([buffer, bytes]);
			let newline = buffer.indexOf(0x0a);
			while (newline >= 0) {
				const line = buffer.subarray(0, newline);
				buffer = buffer.subarray(newline + 1);
				this.processLine(line.at(-1) === 0x0d ? line.subarray(0, -1) : line);
				newline = buffer.indexOf(0x0a);
			}
		}
		if (buffer.length > 0) {
			this.processLine(
				buffer.at(-1) === 0x0d ? buffer.subarray(0, -1) : buffer,
			);
		}
		await this.host.waitForCommands();
		await this.writeQueue;
	}

	dispose(): void {
		this.unsubscribeHost();
		this.host.dispose();
	}

	async abortAndWait(): Promise<void> {
		await this.host.abortAndWait();
		await this.writeQueue;
	}

	private processLine(line: Buffer): void {
		if (line.length === 0) return;
		let parsed: unknown;
		try {
			parsed = JSON.parse(line.toString("utf8"));
		} catch (error) {
			void this.write({
				type: "response",
				command: "parse",
				success: false,
				error: `Failed to parse command: ${error instanceof Error ? error.message : String(error)}`,
			});
			return;
		}
		if (!isRecord(parsed) || typeof parsed.type !== "string") {
			void this.write({
				type: "response",
				command: "parse",
				success: false,
				error: "Command must be an object with a string type",
			});
			return;
		}
		void this.host.handleCommand(parsed as RpcCommand, (response) =>
			this.write(response),
		);
	}

	private write(record: unknown): Promise<void> {
		this.writeQueue = this.writeQueue.then(() => this.writeRecord(record));
		return this.writeQueue;
	}
}


export async function runRpcMode(
	cwd: string,
	options: { model?: string; noSession?: boolean; sessionId?: string } = {},
): Promise<number> {
	const stdout = takeOverStdout();
	let host: Awaited<ReturnType<typeof createHeadlessHost>> | null = null;
	let server: RpcModeServer | null = null;
	let signalExitCode: number | null = null;
	const handleSignal = (signal: "SIGINT" | "SIGTERM") => {
		const exitCode = signal === "SIGINT" ? 130 : 143;
		if (signalExitCode !== null) process.exit(exitCode);
		signalExitCode = exitCode;
		host?.runtime.abort();
		process.stdin.destroy();
	};
	const handleSigint = () => handleSignal("SIGINT");
	const handleSigterm = () => handleSignal("SIGTERM");
	process.on("SIGINT", handleSigint);
	process.on("SIGTERM", handleSigterm);
	let exitCode = 0;
	try {
		const resolved = await resolveHeadlessSession(cwd, {
			defaultPersistence: options.noSession ? "ephemeral" : "persistent",
			sessionId: options.sessionId,
		});
		host = await createHeadlessHost(resolved.session, {
			persistSession: resolved.persistSession,
		});
		await applyStartupModel(host.runtime, options.model);
		server = new RpcModeServer(
			host.runtime,
			process.stdin,
			(record) => stdout.write(`${JSON.stringify(record)}\n`),
			resolved.persistSession,
			host.commands,
			host.waitForWorkspaceReady,
		);
		await server.start();
		await server.abortAndWait();
		exitCode = signalExitCode ?? 0;
	} catch (error) {
		if (signalExitCode !== null) {
			exitCode = signalExitCode;
		} else {
			console.error(error instanceof Error ? error.message : String(error));
			exitCode = 1;
		}
	} finally {
		process.off("SIGINT", handleSigint);
		process.off("SIGTERM", handleSigterm);
		server?.dispose();
		try {
			await host?.dispose();
		} catch (error) {
			console.error(
				`Headless cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
			);
			if (signalExitCode === null) exitCode = 1;
		} finally {
			stdout.restore();
		}
	}
	return exitCode;
}
