import type { AgentMessage } from "../runtime/agent";
import {
	type AgentRuntime,
	AuthenticationRequiredError,
} from "../runtime/agent-runtime";
import type { HeadlessHost } from "./headless-host";
import { createHeadlessHost, takeOverStdout } from "./headless-host";
import { applyStartupModel } from "./headless-model";
import { resolveHeadlessSession } from "./headless-session";

function assistantText(message: AgentMessage | undefined): string {
	if (message?.role !== "assistant") return "";
	return message.content
		.filter(
			(block): block is { type: "text"; text: string } => block.type === "text",
		)
		.map((block) => block.text)
		.join("\n");
}

// Run `bun run smoke:print-mode` from the repository root after changing this
// lifecycle or its plugin, output, signal, tool, or persistence behavior.
export async function runPrintMode(
	prompt: string,
	cwd: string,
	options: { model?: string; noSession?: boolean; sessionId?: string } = {},
): Promise<number> {
	const stdout = takeOverStdout();
	let runtime: AgentRuntime | null = null;
	let host: HeadlessHost | null = null;
	const startupAbort = new AbortController();
	let signalExitCode: number | null = null;
	let forcedExitTimer: ReturnType<typeof setTimeout> | null = null;
	const handleSignal = (signal: "SIGINT" | "SIGTERM") => {
		const exitCode = signal === "SIGINT" ? 130 : 143;
		if (signalExitCode !== null) process.exit(exitCode);
		signalExitCode = exitCode;
		startupAbort.abort();
		runtime?.abort();
		forcedExitTimer = setTimeout(() => process.exit(exitCode), 5_000);
	};
	const handleSigint = () => handleSignal("SIGINT");
	const handleSigterm = () => handleSignal("SIGTERM");
	process.on("SIGINT", handleSigint);
	process.on("SIGTERM", handleSigterm);
	let exitCode = 1;
	try {
		const resolved = await resolveHeadlessSession(cwd, {
			defaultPersistence: options.noSession ? "ephemeral" : "persistent",
			sessionId: options.sessionId,
		});
		if (signalExitCode !== null) {
			exitCode = signalExitCode;
		} else {
			host = await createHeadlessHost(resolved.session, {
				persistSession: resolved.persistSession,
				signal: startupAbort.signal,
			});
			runtime = host.runtime;
			if (signalExitCode === null) {
				await applyStartupModel(runtime, options.model);
			}
			if (signalExitCode === null) {
				await runtime.submitUserMessage(prompt);
			}
			if (signalExitCode !== null) {
				exitCode = signalExitCode;
			} else {
				const lastMessage = runtime.getMessages().at(-1);
				if (lastMessage?.role !== "assistant") {
					console.error("Kit completed without an assistant response.");
				} else if (
					lastMessage.stopReason === "error" ||
					lastMessage.stopReason === "aborted" ||
					lastMessage.stopReason === "pending"
				) {
					console.error(
						lastMessage.errorMessage ?? `Request ${lastMessage.stopReason}.`,
					);
				} else {
					const text = assistantText(lastMessage);
					if (!text.trim()) {
						console.error("Kit completed without assistant text.");
					} else {
						await stdout.write(`${text}\n`);
						exitCode = 0;
					}
				}
			}
		}
	} catch (error) {
		if (signalExitCode !== null) {
			exitCode = signalExitCode;
		} else if (error instanceof AuthenticationRequiredError) {
			console.error(
				"Kit is not authenticated with an available model provider.",
			);
		} else {
			console.error(error instanceof Error ? error.message : String(error));
		}
	} finally {
		process.off("SIGINT", handleSigint);
		process.off("SIGTERM", handleSigterm);
		try {
			await host?.dispose();
		} catch (error) {
			console.error(
				`Headless cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
			);
			if (signalExitCode === null) exitCode = 1;
		} finally {
			if (forcedExitTimer) clearTimeout(forcedExitTimer);
			stdout.restore();
		}
	}
	return exitCode;
}
