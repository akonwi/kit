import type { AgentMessage } from "../runtime/agent";
import {
	type AgentRuntime,
	AuthenticationRequiredError,
} from "../runtime/agent-runtime";
import type { HeadlessHost } from "./headless-host";
import {
	createEphemeralSession,
	createHeadlessHost,
	takeOverStdout,
} from "./headless-host";

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
): Promise<number> {
	const stdout = takeOverStdout();
	let runtime: AgentRuntime | null = null;
	let host: HeadlessHost | null = null;
	let signalExitCode: number | null = null;
	let forcedExitTimer: ReturnType<typeof setTimeout> | null = null;
	const handleSignal = (signal: "SIGINT" | "SIGTERM") => {
		const exitCode = signal === "SIGINT" ? 130 : 143;
		if (signalExitCode !== null) process.exit(exitCode);
		signalExitCode = exitCode;
		runtime?.abort();
		forcedExitTimer = setTimeout(() => process.exit(exitCode), 5_000);
	};
	const handleSigint = () => handleSignal("SIGINT");
	const handleSigterm = () => handleSignal("SIGTERM");
	process.on("SIGINT", handleSigint);
	process.on("SIGTERM", handleSigterm);
	try {
		host = await createHeadlessHost(createEphemeralSession(cwd));
		runtime = host.runtime;

		if (signalExitCode !== null) return signalExitCode;
		await runtime.submitUserMessage(prompt);
		if (signalExitCode !== null) return signalExitCode;
		const lastMessage = runtime.getMessages().at(-1);
		if (lastMessage?.role !== "assistant") {
			console.error("Kit completed without an assistant response.");
			return 1;
		}
		if (
			lastMessage.stopReason === "error" ||
			lastMessage.stopReason === "aborted" ||
			lastMessage.stopReason === "pending"
		) {
			console.error(
				lastMessage.errorMessage ?? `Request ${lastMessage.stopReason}.`,
			);
			return 1;
		}
		const text = assistantText(lastMessage);
		if (!text.trim()) {
			console.error("Kit completed without assistant text.");
			return 1;
		}
		await stdout.write(`${text}\n`);
		return signalExitCode ?? 0;
	} catch (error) {
		if (signalExitCode !== null) return signalExitCode;
		if (error instanceof AuthenticationRequiredError) {
			console.error(
				"Kit is not authenticated with an available model provider.",
			);
		} else {
			console.error(error instanceof Error ? error.message : String(error));
		}
		return 1;
	} finally {
		process.off("SIGINT", handleSigint);
		process.off("SIGTERM", handleSigterm);
		try {
			await host?.dispose();
		} catch (error) {
			console.error(
				`Headless cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
			);
		} finally {
			if (forcedExitTimer) clearTimeout(forcedExitTimer);
			stdout.restore();
		}
	}
}
