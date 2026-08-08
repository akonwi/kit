import { type MermaidPreviewColors, renderMermaidPreviewImage } from "./render";

declare const self: Worker;

export type MermaidPreviewWorkerRequest = {
	source: string;
	colors: MermaidPreviewColors;
};

export type MermaidPreviewWorkerResult =
	| {
			ok: true;
			png: Uint8Array;
			width: number;
			height: number;
	  }
	| { ok: false; error: string };

self.onmessage = async (event: MessageEvent<MermaidPreviewWorkerRequest>) => {
	try {
		const image = await renderMermaidPreviewImage(
			event.data.source,
			event.data.colors,
		);
		self.postMessage(
			{ ok: true, ...image } satisfies MermaidPreviewWorkerResult,
			[image.png.buffer],
		);
	} catch (error) {
		self.postMessage({
			ok: false,
			error: error instanceof Error ? error.message : String(error),
		} satisfies MermaidPreviewWorkerResult);
	}
};
