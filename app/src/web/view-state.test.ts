import { describe, expect, mock, test } from "bun:test";
import { createClientState } from "@akonwi/kit-session-client";
import { WebClientViewState } from "./view-state";

describe("WebClientViewState toast errors", () => {
	test("falls back to status when the toast sink fails", () => {
		const view = new WebClientViewState(createClientState(), () => {
			throw new Error("popover unavailable");
		});

		view.reportError(new Error("Request failed"), "Command failed");

		expect(view.snapshot().status).toEqual({
			message: "Request failed",
			isError: true,
		});
	});

	test("keeps replayed protocol errors visible with a toast sink", () => {
		const state = { ...createClientState(), lastError: "Provider unavailable" };
		const view = new WebClientViewState(state, () => {});

		expect(view.snapshot().status).toEqual({
			message: "Provider unavailable",
			isError: true,
		});
	});

	test("does not emit imperative toasts after disposal", () => {
		const showToast = mock(() => {});
		const view = new WebClientViewState(createClientState(), showToast);
		view.dispose();

		view.reportError(new Error("late failure"));

		expect(showToast).not.toHaveBeenCalled();
	});
});
