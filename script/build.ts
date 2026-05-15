#!/usr/bin/env bun
import { execSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import solidPlugin from "@opentui/solid/bun-plugin";

// Enforce minimum Bun version
const MIN_BUN_VERSION = "1.3.0";
if (Bun.semver.order(Bun.version, MIN_BUN_VERSION) < 0) {
	console.error(
		`Bun >= ${MIN_BUN_VERSION} required (found ${Bun.version}). Please upgrade: bun upgrade`,
	);
	process.exit(1);
}

const dir = path.resolve(import.meta.dirname, "..");
const distDir = path.join(dir, "dist");
const runtimeDir = path.join(distDir, "runtime");
const binaryPath = path.join(distDir, "kit");
const pluginRuntimePath = path.join(distDir, "plugin.js");
const pluginTypesPath = path.join(distDir, "plugin.d.ts");
const pluginTypesSourcePath = path.join(dir, "src/plugins/sdk.ts");
const toastTypesPath = path.join(distDir, "toasts.d.ts");
const toastTypesSourcePath = path.join(dir, "src/state/toasts.ts");
const parserWorkerPath = path.resolve(
	dir,
	"node_modules/@opentui/core/parser.worker.js",
);
const coreAssetsPath = path.resolve(dir, "node_modules/@opentui/core/assets");

process.chdir(dir);

// Bundle and compile with the OpenTUI Solid plugin.
await fs.promises.rm(distDir, { recursive: true, force: true });
await fs.promises.mkdir(distDir, { recursive: true });
await fs.promises.mkdir(runtimeDir, { recursive: true });

console.log("Compiling binary...");

const bundle = await Bun.build({
	target: "bun",
	tsconfig: "./tsconfig.json",
	plugins: [solidPlugin],
	entrypoints: ["./src/app/main.tsx"],
	compile: {
		outfile: binaryPath,
	},
});

if (!bundle.success) {
	console.error("Compile failed:");
	for (const log of bundle.logs) {
		console.error(" ", log);
	}
	process.exit(1);
}

if (bundle.logs.length > 0) {
	for (const log of bundle.logs) {
		console.warn(" ", log);
	}
}

if (!fs.existsSync(binaryPath)) {
	console.error(
		`Compile reported success but binary not found at ${binaryPath}`,
	);
	console.error(`Bun version: ${Bun.version}`);
	console.error(
		"Outputs:",
		bundle.outputs.map((o) => o.path),
	);
	process.exit(1);
}

// Ad-hoc sign the binary so macOS doesn't SIGKILL it
if (process.platform === "darwin") {
	console.log("Code-signing binary for macOS...");
	execSync(`codesign --remove-signature ${binaryPath}`, { stdio: "inherit" });
	execSync(`codesign --sign - --force ${binaryPath}`, { stdio: "inherit" });
}

console.log("Binary compiled, bundling runtime assets...");

// Step 2: Bundle the tree-sitter worker and copy runtime assets needed by the compiled binary.
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
	if (patchedWorkerSource === workerSource) {
		console.warn(
			"Warning: WASM path patch did not match — tree-sitter may not work in compiled binary",
		);
	}
	await fs.promises.writeFile(bundledWorkerPath, patchedWorkerSource, "utf8");
}

await fs.promises.cp(coreAssetsPath, path.join(runtimeDir, "assets"), {
	recursive: true,
});

await fs.promises.writeFile(
	pluginRuntimePath,
	[
		"// Runtime surface for the public @akonwi/kit/plugin SDK.",
		"// Plugin API shapes are type-only; runtime helpers are explicitly exported here.",
		'export { Type } from "typebox";',
		"",
	].join("\n"),
	"utf8",
);
const pluginTypesSource = await fs.promises.readFile(
	pluginTypesSourcePath,
	"utf8",
);
await fs.promises.writeFile(
	pluginTypesPath,
	pluginTypesSource.replace('from "../state/toasts"', 'from "./toasts"'),
	"utf8",
);
await fs.promises.copyFile(toastTypesSourcePath, toastTypesPath);

console.log(`Built ${binaryPath}`);
