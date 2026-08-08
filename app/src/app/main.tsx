import { parseArgs } from "node:util";
import { isValidModelSelector } from "./headless-model";
import { buildPrintModePrompt } from "./print-mode-input";

const { positionals, values } = parseArgs({
	args: process.argv.slice(2),
	options: {
		"allow-host": { type: "string", multiple: true },
		"allow-origin": { type: "string", multiple: true },
		host: { type: "string" },
		mode: { type: "string" },
		model: { type: "string" },
		"no-session": { type: "boolean" },
		port: { type: "string" },
		print: { type: "boolean", short: "p" },
		rpc: { type: "boolean" },
		session: { type: "string", short: "s" },
		version: { type: "boolean", short: "v" },
	},
	strict: false,
	allowPositionals: true,
});

const subcommand = values.version === true ? "version" : positionals[0];
const hasWebOnlyOptions =
	values.host !== undefined ||
	values.port !== undefined ||
	values["allow-host"] !== undefined ||
	values["allow-origin"] !== undefined;

async function readPipedStdin(): Promise<string | undefined> {
	if (process.stdin.isTTY) return undefined;
	process.stdin.setEncoding("utf8");
	let content = "";
	for await (const chunk of process.stdin) content += chunk;
	return content;
}

if (values.mode === "web") {
	const port =
		typeof values.port === "string" && /^\d+$/.test(values.port)
			? Number(values.port)
			: undefined;
	if (values.rpc || values.print || values.version || positionals.length > 0) {
		console.error(
			"kit --mode web cannot be combined with --rpc, --print, --version, or positional arguments",
		);
		process.exitCode = 1;
	} else if (values["no-session"]) {
		console.error("kit --mode web does not support --no-session");
		process.exitCode = 1;
	} else if (
		typeof values.model === "string" &&
		!isValidModelSelector(values.model)
	) {
		console.error("kit --mode web --model expects <provider>/<model-id>");
		process.exitCode = 1;
	} else if (
		values.port !== undefined &&
		(port === undefined || port < 1 || port > 65535)
	) {
		console.error("kit --mode web --port expects an integer from 1 to 65535");
		process.exitCode = 1;
	} else {
		const { safeProcessCwd } = await import("../process-cwd");
		const { runWebMode } = await import("./web-mode");
		process.exitCode = await runWebMode(safeProcessCwd(), {
			allowedHosts: Array.isArray(values["allow-host"])
				? values["allow-host"].filter(
						(host): host is string => typeof host === "string",
					)
				: undefined,
			allowedOrigins: Array.isArray(values["allow-origin"])
				? values["allow-origin"].filter(
						(origin): origin is string => typeof origin === "string",
					)
				: undefined,
			hostname: typeof values.host === "string" ? values.host : undefined,
			port,
			model: typeof values.model === "string" ? values.model : undefined,
			sessionId:
				typeof values.session === "string" ? values.session : undefined,
		});
	}
} else if (values.mode !== undefined) {
	console.error("--mode is only supported for web mode; use --rpc for RPC mode");
	process.exitCode = 1;
} else if (hasWebOnlyOptions) {
	console.error(
		"--host, --port, --allow-host, and --allow-origin require --mode web",
	);
	process.exitCode = 1;
} else if (values.rpc === true && values.print === true) {
	console.error("kit --rpc and --print are mutually exclusive");
	process.exitCode = 1;
} else if (values.rpc === true) {
	if (values.version || positionals.length > 0) {
		console.error(
			"kit --rpc cannot be combined with --version or positional arguments",
		);
		process.exitCode = 1;
	} else if (
		typeof values.model === "string" &&
		!isValidModelSelector(values.model)
	) {
		console.error("kit --rpc --model expects <provider>/<model-id>");
		process.exitCode = 1;
	} else if (values["no-session"] && values.session) {
		console.error("kit --rpc cannot combine --no-session with --session");
		process.exitCode = 1;
	} else {
		const { safeProcessCwd } = await import("../process-cwd");
		const { runRpcMode } = await import("./rpc-mode");
		process.exitCode = await runRpcMode(safeProcessCwd(), {
			model: typeof values.model === "string" ? values.model : undefined,
			noSession: values["no-session"] === true,
			sessionId:
				typeof values.session === "string" ? values.session : undefined,
		});
	}
} else if (values.print === true) {
	if (values.version) {
		console.error("kit -p cannot be combined with --version");
		process.exitCode = 1;
	} else if (
		typeof values.model === "string" &&
		!isValidModelSelector(values.model)
	) {
		console.error("kit -p --model expects <provider>/<model-id>");
		process.exitCode = 1;
	} else if (values["no-session"] && values.session) {
		console.error("kit -p cannot combine --no-session with --session");
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
			process.exitCode = await runPrintMode(prompt, safeProcessCwd(), {
				model: typeof values.model === "string" ? values.model : undefined,
				noSession: values["no-session"] === true,
				sessionId:
					typeof values.session === "string" ? values.session : undefined,
			});
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
