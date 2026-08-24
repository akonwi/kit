import { describe, expect, test } from "bun:test";
import {
	resolveReviewPaneVisibility,
	toggleReviewTreeState,
} from "./ReviewContent";

describe("code review pane visibility", () => {
	test("shows the expanded changes tree beside the diff in wide layouts", () => {
		expect(
			resolveReviewPaneVisibility({
				wide: true,
				mode: "patch",
				treeExpanded: true,
			}),
		).toEqual({ tree: true, diff: true });
	});

	test("collapses the changes tree without hiding the wide diff", () => {
		expect(
			resolveReviewPaneVisibility({
				wide: true,
				mode: "patch",
				treeExpanded: false,
			}),
		).toEqual({ tree: false, diff: true });
	});

	test("uses full-width tree and diff modes in narrow layouts", () => {
		expect(
			resolveReviewPaneVisibility({
				wide: false,
				mode: "tree",
				treeExpanded: false,
			}),
		).toEqual({ tree: true, diff: false });
		expect(
			resolveReviewPaneVisibility({
				wide: false,
				mode: "patch",
				treeExpanded: true,
			}),
		).toEqual({ tree: false, diff: true });
	});

	test("keeps the diff visible when a tree-initiated editor becomes narrow", () => {
		expect(
			resolveReviewPaneVisibility({
				wide: false,
				mode: "tree",
				treeExpanded: true,
				editorOpen: true,
			}),
		).toEqual({ tree: false, diff: true });
		expect(
			resolveReviewPaneVisibility({
				wide: true,
				mode: "tree",
				treeExpanded: true,
				editorOpen: true,
			}),
		).toEqual({ tree: true, diff: true });
	});

	test("keeps tree expansion and focus mode aligned across breakpoints", () => {
		const collapsedWide = toggleReviewTreeState({
			wide: true,
			mode: "patch",
			treeExpanded: true,
		});
		expect(collapsedWide).toEqual({ mode: "patch", treeExpanded: false });

		const openedNarrow = toggleReviewTreeState({
			wide: false,
			...collapsedWide,
		});
		expect(openedNarrow).toEqual({ mode: "tree", treeExpanded: true });
		expect(
			resolveReviewPaneVisibility({ wide: true, ...openedNarrow }),
		).toEqual({ tree: true, diff: true });
	});
});
