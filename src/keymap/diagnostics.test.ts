import { describe, expect, mock, test } from "bun:test";
import {
	createKeybindingDiagnosticReporter,
	formatKeybindingDiagnostic,
} from "./diagnostics";

describe("keybinding diagnostics", () => {
	test("formats unknown command diagnostics", () => {
		expect(
			formatKeybindingDiagnostic({
				type: "unknown",
				command: "composer.typo",
				message: "Unknown keybinding command",
			}),
		).toBe("composer.typo: Unknown keybinding command");
	});

	test("formats invalid binding diagnostics", () => {
		expect(
			formatKeybindingDiagnostic({
				type: "invalid",
				command: "composer.abort",
				key: "ctrl+shift",
				message: "missing key name",
			}),
		).toBe("ctrl+shift for composer.abort: missing key name");
	});

	test("formats duplicate binding diagnostics", () => {
		expect(
			formatKeybindingDiagnostic({
				type: "duplicate",
				command: "picker.select",
				key: "enter",
				existingCommand: "picker.submit-input",
				existingKey: "return",
			}),
		).toBe(
			"enter for picker.select conflicts with return for picker.submit-input",
		);
	});

	test("reports every diagnostic as a toast", () => {
		const showToast = mock(() => {});
		const report = createKeybindingDiagnosticReporter(showToast);
		const diagnostic = {
			type: "invalid" as const,
			command: "composer.abort",
			key: "ctrl+shift",
			message: "missing key name",
		};

		report(diagnostic);
		report(diagnostic);

		expect(showToast).toHaveBeenCalledTimes(2);
		expect(showToast).toHaveBeenNthCalledWith(1, {
			variant: "warning",
			title: "Keybinding ignored",
			subtitle: "ctrl+shift for composer.abort: missing key name",
		});
	});
});
