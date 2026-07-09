import { parseArgs } from "node:util";

const { positionals, values } = parseArgs({
	args: process.argv.slice(2),
	options: {
		session: { type: "string", short: "s" },
		version: { type: "boolean", short: "v" },
	},
	strict: false,
	allowPositionals: true,
});

const subcommand = values.version === true ? "version" : positionals[0];

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
