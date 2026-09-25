export async function buildWebTuiClient(options?: {
	minify?: boolean;
}): Promise<string> {
	const result = await Bun.build({
		entrypoints: [new URL("./client.ts", import.meta.url).pathname],
		target: "browser",
		format: "esm",
		conditions: ["browser", "production"],
		minify: options?.minify ?? false,
	});
	if (!result.success) {
		throw new AggregateError(result.logs, "Web TUI client bundle failed");
	}
	const output = result.outputs.find((candidate) =>
		candidate.path.endsWith("client.js"),
	);
	if (!output) throw new Error("Web TUI client bundle produced no JavaScript");
	return output.text();
}
