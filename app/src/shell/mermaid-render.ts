import initMerman, {
	renderAscii,
} from "@mermanjs/web/pkg/ascii/merman_wasm.js";
import mermanWasmPath from "@mermanjs/web/pkg/ascii/merman_wasm_bg.wasm" with {
	type: "file",
};

const MAX_MERMAID_OUTPUT_LINES = 120;
const MAX_MERMAID_OUTPUT_COLUMNS = 240;

const mermanReady = initMerman({
	module_or_path: Bun.file(
		new URL(mermanWasmPath, import.meta.url),
	).arrayBuffer(),
});

export async function renderMermaidCode(
	source: string,
): Promise<string | null> {
	await mermanReady;
	try {
		const output = renderAscii(source);
		const lines = output.split("\n");
		if (
			lines.length > MAX_MERMAID_OUTPUT_LINES ||
			lines.some((line) => Bun.stringWidth(line) > MAX_MERMAID_OUTPUT_COLUMNS)
		) {
			return null;
		}
		return output;
	} catch {
		return null;
	}
}
