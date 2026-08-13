import { transformAsync } from "@babel/core";
// @ts-expect-error: @babel/preset-typescript does not publish declarations.
import presetTypeScript from "@babel/preset-typescript";
// @ts-expect-error: babel-preset-solid does not publish TypeScript declarations.
import presetSolid from "babel-preset-solid";
import type { BunPlugin } from "bun";

const WEB_TSX = /[/\\]src[/\\]web[/\\].*\.tsx$/;

const solidWebPlugin: BunPlugin = {
	name: "solid-web",
	setup(build) {
		build.onLoad({ filter: WEB_TSX }, async ({ path }) => {
			const source = await Bun.file(path).text();
			const transformed = await transformAsync(source, {
				filename: path,
				babelrc: false,
				configFile: false,
				presets: [
					[
						presetSolid,
						{
							generate: "dom",
							hydratable: false,
							moduleName: "solid-js/web",
						},
					],
					[presetTypeScript, { allExtensions: true, isTSX: true }],
				],
			});
			if (!transformed?.code) {
				throw new Error(`Solid compilation produced no output for ${path}`);
			}
			return { contents: transformed.code, loader: "js" };
		});
	},
};

export async function buildWebClient(options?: {
	minify?: boolean;
}): Promise<string> {
	const result = await Bun.build({
		entrypoints: [new URL("./client.tsx", import.meta.url).pathname],
		target: "browser",
		format: "esm",
		conditions: ["browser", "production"],
		minify: options?.minify ?? false,
		plugins: [solidWebPlugin],
	});
	if (!result.success) {
		throw new AggregateError(result.logs, "Web client bundle failed");
	}
	const output = result.outputs.find((candidate) =>
		candidate.path.endsWith("client.js"),
	);
	if (!output)
		throw new Error("Web client bundle produced no JavaScript output");
	return output.text();
}
