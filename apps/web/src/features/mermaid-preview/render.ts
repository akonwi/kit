import interFontPath from "@fontsource-variable/inter/files/inter-latin-wght-normal.woff2" with {
	type: "file",
};
import initMerman, {
	renderSvg,
} from "@mermanjs/web/pkg/render-only/merman_wasm.js";
import mermanWasmPath from "@mermanjs/web/pkg/render-only/merman_wasm_bg.wasm" with {
	type: "file",
};
import { initWasm, Resvg, type ResvgRenderOptions } from "@resvg/resvg-wasm";
import resvgWasmPath from "@resvg/resvg-wasm/index_bg.wasm" with {
	type: "file",
};

const MAX_SOURCE_BYTES = 64_000;
const MAX_SVG_BYTES = 5_000_000;
const MAX_PNG_BYTES = 8_000_000;
const TARGET_PNG_WIDTH = 1_600;
const MAX_PNG_WIDTH = 2_000;
const MAX_PNG_HEIGHT = 8_000;
const MAX_PNG_PIXELS = 10_000_000;
const textEncoder = new TextEncoder();

function appearanceForBackground(background: string): "light" | "dark" {
	const match = background.match(/^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/i);
	if (!match) return "dark";
	const [, red = "00", green = "00", blue = "00"] = match;
	const luminance =
		(0.2126 * Number.parseInt(red, 16) +
			0.7152 * Number.parseInt(green, 16) +
			0.0722 * Number.parseInt(blue, 16)) /
		255;
	return luminance >= 0.5 ? "light" : "dark";
}

export type MermaidPreviewColors = {
	background: string;
	surface: string;
	text: string;
	mutedText: string;
	border: string;
	line: string;
};

export type MermaidPreviewImage = {
	png: Uint8Array;
	width: number;
	height: number;
};

const rendererReady = Promise.all([
	initMerman({
		module_or_path: Bun.file(
			new URL(mermanWasmPath, import.meta.url),
		).arrayBuffer(),
	}),
	initWasm(Bun.file(new URL(resvgWasmPath, import.meta.url)).arrayBuffer()),
	Bun.file(new URL(interFontPath, import.meta.url)).bytes(),
]);

export async function renderMermaidPreviewImage(
	source: string,
	colors: MermaidPreviewColors,
): Promise<MermaidPreviewImage> {
	if (textEncoder.encode(source).byteLength > MAX_SOURCE_BYTES) {
		throw new Error("Mermaid source exceeds the visual preview limit");
	}

	const [, , font] = await rendererReady;
	let svg: string;
	try {
		svg = renderSvg(
			source,
			JSON.stringify({
				resources: {
					profile: "interactive",
					max_source_bytes: MAX_SOURCE_BYTES,
					max_flowchart_nodes: 300,
					max_flowchart_edges: 500,
					max_flowchart_subgraphs: 100,
					max_class_nodes: 300,
					max_class_edges: 500,
					max_class_namespaces: 100,
					max_label_bytes: 4_000,
				},
				host_theme: {
					appearance: appearanceForBackground(colors.background),
					font_family: "Inter",
					roles: {
						canvas: colors.background,
						surface: colors.surface,
						surface_alt: colors.surface,
						surface_muted: colors.surface,
						text: colors.text,
						subtle_text: colors.mutedText,
						border: colors.border,
						line: colors.line,
						edge_label_background: colors.background,
						cluster_background: colors.surface,
						cluster_border: colors.border,
						note_background: colors.surface,
						note_border: colors.border,
						note_text: colors.text,
						actor_background: colors.surface,
						actor_border: colors.border,
						actor_text: colors.text,
						activation_background: colors.surface,
						activation_border: colors.border,
					},
					output: {
						pipeline: "resvg-safe",
						root_background: "canvas",
						drop_native_duplicate_fallbacks: true,
					},
				},
				svg: {
					pipeline: "resvg-safe",
					root_background_color: colors.background,
					drop_native_duplicate_fallbacks: true,
				},
			}),
		);
	} catch (error) {
		const message =
			typeof error === "object" &&
			error !== null &&
			"message" in error &&
			typeof error.message === "string"
				? error.message
				: String(error);
		throw new Error(message);
	}
	if (textEncoder.encode(svg).byteLength > MAX_SVG_BYTES) {
		throw new Error("Rendered Mermaid SVG exceeds the preview limit");
	}

	const baseOptions = {
		background: colors.background,
		font: {
			fontBuffers: [font],
			defaultFontFamily: "Inter",
			loadSystemFonts: false,
		},
		shapeRendering: 2,
		textRendering: 1,
		imageRendering: 0,
	} satisfies ResvgRenderOptions;
	let renderer = new Resvg(svg, {
		...baseOptions,
		fitTo: { mode: "original" },
	});
	const scale = Math.min(
		1,
		TARGET_PNG_WIDTH / renderer.width,
		MAX_PNG_WIDTH / renderer.width,
		MAX_PNG_HEIGHT / renderer.height,
		Math.sqrt(MAX_PNG_PIXELS / (renderer.width * renderer.height)),
	);
	if (scale < 1) {
		renderer.free();
		renderer = new Resvg(svg, {
			...baseOptions,
			fitTo: { mode: "zoom", value: scale },
		});
	}
	try {
		if (renderer.imagesToResolve().length > 0) {
			throw new Error("External images are not allowed in Mermaid previews");
		}
		const rendered = renderer.render();
		try {
			if (
				rendered.width > MAX_PNG_WIDTH ||
				rendered.height > MAX_PNG_HEIGHT ||
				rendered.width * rendered.height > MAX_PNG_PIXELS
			) {
				throw new Error(
					"Rendered Mermaid image exceeds the preview dimensions",
				);
			}
			const png = Uint8Array.from(rendered.asPng());
			if (png.byteLength > MAX_PNG_BYTES) {
				throw new Error("Rendered Mermaid PNG exceeds the preview limit");
			}
			return { png, width: rendered.width, height: rendered.height };
		} finally {
			rendered.free();
		}
	} finally {
		renderer.free();
	}
}
