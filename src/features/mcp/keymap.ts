import type { KeybindingDiagnostic } from "../../keymap/bindings";
import { createKeybindingDiagnosticReporter } from "../../keymap/diagnostics";
import type { InternalPluginAPI } from "../../plugins";

export type McpKeymapProps = {
	settings: ReturnType<InternalPluginAPI["settings"]["get"]>;
	onKeybindingDiagnostic: (diagnostic: KeybindingDiagnostic) => void;
};

export function createMcpKeymapProps(kit: InternalPluginAPI): McpKeymapProps {
	return {
		settings: kit.settings.get(),
		onKeybindingDiagnostic: createKeybindingDiagnosticReporter(kit.ui.toast),
	};
}
