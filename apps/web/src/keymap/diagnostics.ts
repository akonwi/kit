import type { ToastInput } from "../state/toasts";
import type { KeybindingDiagnostic } from "./bindings";

export type KeybindingDiagnosticReporter = (
	diagnostic: KeybindingDiagnostic,
) => void;

type ShowToast = (toast: ToastInput) => void;

export function formatKeybindingDiagnostic(
	diagnostic: KeybindingDiagnostic,
): string {
	if (diagnostic.type === "unknown") {
		return `${diagnostic.command}: ${diagnostic.message}`;
	}
	if (diagnostic.type === "invalid") {
		return `${diagnostic.key} for ${diagnostic.command}: ${diagnostic.message}`;
	}
	return `${diagnostic.key} for ${diagnostic.command} conflicts with ${diagnostic.existingKey} for ${diagnostic.existingCommand}`;
}

export function createKeybindingDiagnosticReporter(
	showToast: ShowToast,
): KeybindingDiagnosticReporter {
	return (diagnostic) => {
		showToast({
			variant: "warning",
			title: "Keybinding ignored",
			subtitle: formatKeybindingDiagnostic(diagnostic),
		});
	};
}

export function reportKeybindingDiagnostics(
	diagnostics: readonly KeybindingDiagnostic[],
	report?: KeybindingDiagnosticReporter,
): void {
	if (!report) return;
	for (const diagnostic of diagnostics) report(diagnostic);
}
