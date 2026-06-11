export type KeymapLayerScope =
	| "app"
	| "composer"
	| "picker"
	| "panel"
	| "modal"
	| "overlay";

export type KeymapLayerPrecedence = "default" | "contextual" | "fallback";

// Panel sits below picker/composer so the cascade dispatches
// command-palette.close / picker.close / composer.abort first when
// they're applicable, then falls through to e.g. turn-activity.close
// only if no input or picker layer claimed the key. Modal still wins
// ESC over a panel because modal > panel.
const SCOPE_PRIORITY: Record<KeymapLayerScope, number> = {
	overlay: 250,
	modal: 200,
	app: 100,
	composer: 90,
	picker: 70,
	panel: 60,
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
