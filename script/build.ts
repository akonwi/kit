#!/usr/bin/env bun
import fs from "node:fs";
import path from "node:path";
import solidPlugin from "@opentui/solid/bun-plugin";
import { $ } from "bun";

const dir = path.resolve(import.meta.dirname, "..");
const distDir = path.join(dir, "dist");
const bundleDir = path.join(distDir, "bundle");
const runtimeDir = path.join(distDir, "runtime");
const binaryPath = path.join(distDir, "kit");
const parserWorkerPath = path.resolve(
	dir,
	"node_modules/@opentui/core/parser.worker.js",
);
const coreAssetsPath = path.resolve(dir, "node_modules/@opentui/core/assets");

process.chdir(dir);

// Step 1: Bundle the app with the OpenTUI Solid plugin.
await fs.promises.rm(distDir, { recursive: true, force: true });
await fs.promises.mkdir(distDir, { recursive: true });
await fs.promises.mkdir(runtimeDir, { recursive: true });

const bundle = await Bun.build({
	target: "bun",
	tsconfig: "./tsconfig.json",
	plugins: [solidPlugin],
	outdir: bundleDir,
	entrypoints: ["./src/app/main.tsx"],
	define: {
		OTUI_TREE_SITTER_WORKER_PATH: JSON.stringify(parserWorkerPath),
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

// Step 2: Compile the bundled JS into a standalone binary.
await $`bun build --compile --target=bun --outfile=${binaryPath} ${path.join(bundleDir, "main.js")}`;

// Step 3: Bundle the tree-sitter worker and copy runtime assets needed by the compiled binary.
const workerBundle = await Bun.build({
	entrypoints: [parserWorkerPath],
	outdir: runtimeDir,
	target: "bun",
});

if (!workerBundle.success) {
	console.error("Worker bundle failed:");
	for (const log of workerBundle.logs) {
		console.error(" ", log);
	}
	process.exit(1);
}

const bundledWorkerPath = path.join(runtimeDir, "parser.worker.js");
const bundledWasm = workerBundle.outputs.find((output) =>
	output.path.endsWith(".wasm"),
);
if (bundledWasm) {
	const wasmFileName = path.basename(bundledWasm.path);
	const workerSource = await fs.promises.readFile(bundledWorkerPath, "utf8");
	const patchedWorkerSource = workerSource.replace(
		`module2.exports = "./${wasmFileName}";`,
		`module2.exports = new URL("./${wasmFileName}", import.meta.url).pathname;`,
	);
	await fs.promises.writeFile(bundledWorkerPath, patchedWorkerSource, "utf8");
}

await fs.promises.cp(coreAssetsPath, path.join(runtimeDir, "assets"), {
	recursive: true,
});

// Clean up intermediate bundle.
await fs.promises.rm(bundleDir, {
	recursive: true,
	force: true,
});

console.log(`Built ${binaryPath}`);
