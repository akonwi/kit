export type KeymapLayerScope =
	| "app"
	| "composer"
	| "picker"
	| "modal"
	| "overlay";

export type KeymapLayerPrecedence = "default" | "contextual" | "fallback";

const SCOPE_PRIORITY: Record<KeymapLayerScope, number> = {
	overlay: 250,
	modal: 200,
	app: 100,
	composer: 90,
	picker: 70,
};

const PRECEDENCE_OFFSET: Record<KeymapLayerPrecedence, number> = {
	default: 0,
	contextual: -10,
	fallback: -20,
};

export function resolveKeymapLayerPriority(
	scope: KeymapLayerScope,
	precedence: KeymapLayerPrecedence = "default",
): number {
	return SCOPE_PRIORITY[scope] + PRECEDENCE_OFFSET[precedence];
}
