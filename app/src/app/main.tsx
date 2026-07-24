import { parseArgs } from "node:util";
import { buildPrintModePrompt } from "./print-mode-input";

const { positionals, values } = parseArgs({
	args: process.argv.slice(2),
	options: {
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

if (values.print === true) {
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
