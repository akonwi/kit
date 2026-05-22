import { parseArgs } from "node:util";

const { positionals } = parseArgs({
	args: process.argv.slice(2),
	options: {
		session: { type: "string", short: "s" },
	},
	strict: false,
	allowPositionals: true,
});

const subcommand = positionals[0];

switch (subcommand) {
	case "threads": {
		const { showThreadPicker } = await import("./threads");
		const sessionId = await showThreadPicker();
		if (sessionId) {
			const { bootstrap } = await import("./bootstrap");
			await bootstrap({ sessionId });
		}
		break;
	}
	default: {
		const { bootstrap } = await import("./bootstrap");
		await bootstrap();
	}
}
