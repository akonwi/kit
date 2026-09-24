import type { CliRenderer } from "@opentui/core";
import {
	registerAliasesField,
	registerBackspacePopsPendingSequence,
	registerBindingOverrides,
	registerCommaBindings,
	registerEscapeClearsPendingSequence,
	registerNeovimDisambiguation,
} from "@opentui/keymap/addons";
import { createDefaultOpenTuiKeymap } from "@opentui/keymap/opentui";

export const KIT_KEY_ALIASES = {
	enter: "return",
} as const;

/**
 * Creates Kit's shared keymap runtime. Keep host/addon setup centralized so
 * feature code only declares command layers and user overrides can stay uniform.
 */
export function createKitKeymap(renderer: CliRenderer) {
	const keymap = createDefaultOpenTuiKeymap(renderer);

	registerCommaBindings(keymap);
	registerAliasesField(keymap);
	registerBindingOverrides(keymap);
	registerEscapeClearsPendingSequence(keymap);
	registerBackspacePopsPendingSequence(keymap);
	registerNeovimDisambiguation(keymap, { timeoutMs: 1000 });

	return keymap;
}
