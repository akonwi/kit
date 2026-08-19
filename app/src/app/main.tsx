import { parseArgs } from "node:util";
import { isValidModelSelector } from "./headless-model";
import { buildPrintModePrompt } from "./print-mode-input";
import { normalizePublicUrl } from "./web-access-policy";

const cliArgs = process.argv.slice(2);
const { positionals, values } = parseArgs({
	args: cliArgs,
	options: {
		"allow-host": { type: "string", multiple: true },
		auth: { type: "string" },
		"experimental-tui": { type: "boolean" },
		"allow-origin": { type: "string", multiple: true },
		host: { type: "string" },
		mode: { type: "string" },
		model: { type: "string" },
		"no-session": { type: "boolean" },
		port: { type: "string" },
		"public-url": { type: "string" },
		print: { type: "boolean", short: "p" },
		rpc: { type: "boolean" },
		session: { type: "string", short: "s" },
		version: { type: "boolean", short: "v" },
		web: { type: "boolean" },
	},
	strict: false,
	allowPositionals: true,
});

const subcommand = values.version === true ? "version" : positionals[0];
const selectsNewSession = cliArgs[0] === "new";
const hasOnlyNewSessionPositional =
	selectsNewSession && positionals.length === 1;
const hasWebOnlyOptions =
	values.auth !== undefined ||
	values.host !== undefined ||
	values.port !== undefined ||
	values["public-url"] !== undefined ||
	values["allow-host"] !== undefined ||
	values["allow-origin"] !== undefined ||
	values["experimental-tui"] !== undefined;
const selectedModes = [values.print, values.rpc, values.web].filter(
	(value) => value === true,
).length;

async function readPipedStdin(): Promise<string | undefined> {
	if (process.stdin.isTTY) return undefined;
	process.stdin.setEncoding("utf8");
	let content = "";
	for await (const chunk of process.stdin) content += chunk;
	return content;
}

function hasControlCharacter(value: string): boolean {
	return Array.from(value).some((character) => {
		const codePoint = character.codePointAt(0) ?? 0;
		return codePoint <= 0x1f || codePoint === 0x7f;
	});
}

function parseBasicAuth(value: unknown):
	| {
			username: string;
			password: string;
	  }
	| undefined {
	if (typeof value !== "string" || value.length > 1_280) return undefined;
	const separator = value.indexOf(":");
	if (separator <= 0 || separator === value.length - 1) return undefined;
	const username = value.slice(0, separator);
	const password = value.slice(separator + 1);
	if (
		username.length > 256 ||
		password.length > 1_024 ||
		hasControlCharacter(username) ||
		hasControlCharacter(password)
	) {
		return undefined;
	}
	return { username, password };
}

if (values.mode !== undefined) {
	console.error("--mode is no longer supported; use --web or --rpc");
	process.exitCode = 1;
} else if (selectedModes > 1) {
	console.error("kit --print, --rpc, and --web are mutually exclusive");
	process.exitCode = 1;
} else if (values.web === true) {
	const basicAuth =
		values.auth === undefined ? undefined : parseBasicAuth(values.auth);
	const port =
		typeof values.port === "string" && /^\d+$/.test(values.port)
			? Number(values.port)
			: undefined;
	const publicUrl =
		typeof values["public-url"] === "string"
			? normalizePublicUrl(values["public-url"])
			: undefined;
	if (
		values.version ||
		(positionals.length > 0 && !hasOnlyNewSessionPositional)
	) {
		console.error(
			"kit --web cannot be combined with --version or positional arguments other than new",
		);
		process.exitCode = 1;
	} else if (selectsNewSession && values.session) {
		console.error("kit new --web cannot combine with --session");
		process.exitCode = 1;
	} else if (values["no-session"] && values.session) {
		console.error("kit --web cannot combine --no-session with --session");
		process.exitCode = 1;
	} else if (values.auth !== undefined && !basicAuth) {
		console.error("kit --web --auth expects <username>:<password>");
		process.exitCode = 1;
	} else if (values["public-url"] !== undefined && !publicUrl) {
		console.error(
			"kit --web --public-url expects an HTTP(S) origin without a path, query, credentials, or fragment",
		);
		process.exitCode = 1;
	} else if (
		typeof values.model === "string" &&
		!isValidModelSelector(values.model)
	) {
		console.error("kit --web --model expects <provider>/<model-id>");
		process.exitCode = 1;
	} else if (
		values.port !== undefined &&
		(port === undefined || port < 1 || port > 65535)
	) {
		console.error("kit --web --port expects an integer from 1 to 65535");
		process.exitCode = 1;
	} else if (values["experimental-tui"] === true) {
		if (typeof values.model === "string") {
			console.error(
				"kit --web --experimental-tui does not support --model; select the model inside the TUI",
			);
			process.exitCode = 1;
		} else {
			const { runWebTuiMode } = await import("./web-tui-mode");
			process.exitCode = await runWebTuiMode({
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
				basicAuth,
				hostname: typeof values.host === "string" ? values.host : undefined,
				port,
				publicUrl: publicUrl ?? undefined,
				newSession: selectsNewSession,
				noSession: values["no-session"] === true,
				sessionId:
					typeof values.session === "string" ? values.session : undefined,
			});
		}
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
			basicAuth,
			hostname: typeof values.host === "string" ? values.host : undefined,
			port,
			publicUrl: publicUrl ?? undefined,
			model: typeof values.model === "string" ? values.model : undefined,
			newSession: selectsNewSession,
			noSession: values["no-session"] === true,
			sessionId:
				typeof values.session === "string" ? values.session : undefined,
		});
	}
} else if (hasWebOnlyOptions) {
	console.error(
		"--auth, --host, --port, --public-url, --allow-host, --allow-origin, and --experimental-tui require --web",
	);
	process.exitCode = 1;
} else if (values.rpc === true) {
	if (
		values.version ||
		(positionals.length > 0 && !hasOnlyNewSessionPositional)
	) {
		console.error(
			"kit --rpc cannot be combined with --version or positional arguments other than new",
		);
		process.exitCode = 1;
	} else if (selectsNewSession && values.session) {
		console.error("kit new --rpc cannot combine with --session");
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
			newSession: selectsNewSession,
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
	} else if (selectsNewSession && values.session) {
		console.error("kit new -p cannot combine with --session");
		process.exitCode = 1;
	} else if (values["no-session"] && values.session) {
		console.error("kit -p cannot combine --no-session with --session");
		process.exitCode = 1;
	} else {
		const stdin = await readPipedStdin();
		const promptPositionals = selectsNewSession
			? positionals.slice(1)
			: positionals;
		const prompt = buildPrintModePrompt(stdin, promptPositionals);
		if (!prompt.trim()) {
			console.error('Usage: kit -p "prompt"');
			process.exitCode = 1;
		} else {
			const { safeProcessCwd } = await import("../process-cwd");
			const { runPrintMode } = await import("./print-mode");
			process.exitCode = await runPrintMode(prompt, safeProcessCwd(), {
				model: typeof values.model === "string" ? values.model : undefined,
				newSession: selectsNewSession,
				noSession: values["no-session"] === true,
				sessionId:
					typeof values.session === "string" ? values.session : undefined,
			});
		}
	}
} else if (values["no-session"] && values.session) {
	console.error("kit cannot combine --no-session with --session");
	process.exitCode = 1;
} else {
	switch (subcommand) {
		case "version": {
			const { version } = await import("../../package.json");
			console.log(`kit v${version}`);
			break;
		}
		case "threads": {
			if (values["no-session"]) {
				console.error("kit threads cannot combine with --no-session");
				process.exitCode = 1;
				break;
			}
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
			await bootstrap({
				newSession: true,
				noSession: values["no-session"] === true,
			});
			break;
		}
		default: {
			const { bootstrap } = await import("./bootstrap");
			await bootstrap({ noSession: values["no-session"] === true });
		}
	}
}
