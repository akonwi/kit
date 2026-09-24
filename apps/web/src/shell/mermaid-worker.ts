import { renderMermaidCode } from "./mermaid-render";

declare const self: Worker;

self.onmessage = async (event: MessageEvent<string>) => {
	try {
		self.postMessage({ ok: true, output: await renderMermaidCode(event.data) });
	} catch {
		self.postMessage({ ok: false });
	}
};
