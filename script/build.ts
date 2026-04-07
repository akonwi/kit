#!/usr/bin/env bun
import fs from "node:fs";
/**
 * Build a standalone pi-kit binary using bun build --compile.
 * Uses the @opentui/solid bun-plugin to handle Solid JSX transforms.
 *
 * The plugin is applied via a preload file since bun build CLI
 * doesn't support plugins directly — we compile a pre-transformed
 * entrypoint first, then build the binary from that.
 */
import path from "node:path";
import solidPlugin from "@opentui/solid/bun-plugin";
import { $ } from "bun";

const dir = path.resolve(import.meta.dirname, "..");
process.chdir(dir);

// Step 1: Use Bun.build with the solid plugin to produce a bundled JS file
const parserWorkerPath = fs.realpathSync(
	path.resolve(dir, "node_modules/@opentui/core/parser.worker.js"),
);

await fs.promises.rm(path.join(dir, "dist"), { recursive: true, force: true });
await fs.promises.mkdir(path.join(dir, "dist"), { recursive: true });

const bundle = await Bun.build({
	target: "bun",
	tsconfig: "./tsconfig.json",
	plugins: [solidPlugin],
	outdir: path.join(dir, "dist", "bundle"),
	entrypoints: ["./src/app/main.tsx"],
	define: {
		OTUI_TREE_SITTER_WORKER_PATH: JSON.stringify(
			path.resolve(dir, "node_modules/@opentui/core/parser.worker.js"),
		),
	},
});

if (!bundle.success) {
	console.error("Bundle failed:");
	for (const log of bundle.logs) {
		console.error(" ", log);
	}
	process.exit(1);
}

console.log("Bundle produced, compiling binary...");

// Step 2: Compile the bundled JS into a standalone binary
await $`bun build --compile --target=bun --outfile=dist/pi-kit dist/bundle/main.js`;

// Clean up intermediate bundle
await fs.promises.rm(path.join(dir, "dist", "bundle"), {
	recursive: true,
	force: true,
});

console.log("Built dist/pi-kit");
