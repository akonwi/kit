import { parseArgs } from "node:util";
import { buildPrintModePrompt } from "./print-mode-input";

const { positionals, values } = parseArgs({
	args: process.argv.slice(2),
	options: {
		mode: { type: "string" },
		"no-session": { type: "boolean" },
		print: { type: "boolean", short: "p" },
		session: { type: "string", short: "s" },
		version: { type: "boolean", short: "v" },
	},
	strict: false,
	allowPositionals: true,
});

const subcommand = values.version === true ? "version" : positionals[0];

async function readPipedStdin(): Promise<string | undefined> {
	if (process.stdin.isTTY) return undefined;
	process.stdin.setEncoding("utf8");
	let content = "";
	for await (const chunk of process.stdin) content += chunk;
	return content;
}

if (values.mode === "rpc") {
	if (values.print || values.version || positionals.length > 0) {
		console.error(
			"kit --mode rpc cannot be combined with --print, --version, or positional arguments",
		);
		process.exitCode = 1;
	} else if (values["no-session"] && values.session) {
		console.error("kit --mode rpc cannot combine --no-session with --session");
		process.exitCode = 1;
	} else {
		const { safeProcessCwd } = await import("../process-cwd");
		const { runRpcMode } = await import("./rpc-mode");
		process.exitCode = await runRpcMode(safeProcessCwd(), {
			noSession: values["no-session"] === true,
			sessionId:
				typeof values.session === "string" ? values.session : undefined,
		});
	}
} else if (values.mode !== undefined) {
	console.error(`Unknown mode: ${String(values.mode)}`);
	process.exitCode = 1;
} else if (values.print === true) {
	if (values.session || values.version) {
		console.error("kit -p cannot be combined with --session or --version");
		process.exitCode = 1;
	} else {
		const stdin = await readPipedStdin();
		const prompt = buildPrintModePrompt(stdin, positionals);
		if (!prompt.trim()) {
			console.error('Usage: kit -p "prompt"');
			process.exitCode = 1;
		} else {
			const { safeProcessCwd } = await import("../process-cwd");
			const { runPrintMode } = await import("./print-mode");
			process.exitCode = await runPrintMode(prompt, safeProcessCwd());
		}
	}
} else {
	switch (subcommand) {
		case "version": {
			const { version } = await import("../../package.json");
			console.log(`kit v${version}`);
			break;
		}
		case "threads": {
			const { showThreadPicker } = await import("./threads");
			const sessionId = await showThreadPicker();
			if (sessionId) {
				const { bootstrap } = await import("./bootstrap");
				await bootstrap({ sessionId });
			}
			break;
		}
		case "new": {
			const { bootstrap } = await import("./bootstrap");
			await bootstrap({ newSession: true });
			break;
		}
		default: {
			const { bootstrap } = await import("./bootstrap");
			await bootstrap();
		}
	}
}
