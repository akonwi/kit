export type OpenMermaidPreview = (source: string) => void;

let openHandler: OpenMermaidPreview | undefined;

export function registerMermaidPreviewHandler(
	handler: OpenMermaidPreview,
): () => void {
	openHandler = handler;
	return () => {
		if (openHandler === handler) openHandler = undefined;
	};
}

export function hasMermaidPreviewHandler(): boolean {
	return openHandler !== undefined;
}

export function requestMermaidPreview(source: string): void {
	openHandler?.(source);
}
