/** @jsxImportSource solid-js */
import {
	FileDiff,
	type FileDiffMetadata,
	parsePatchFiles,
	type ThemeTypes,
} from "@pierre/diffs";
import { createEffect, type JSX, onCleanup } from "solid-js";
import { readBrowserTheme } from "./browser-theme";
import type { RemoteReviewFile } from "./remote-services";

export type ReviewDiffLayout = "unified" | "split";
export type ReviewDiffOverflow = "scroll" | "wrap";

function themeType(): ThemeTypes {
	return readBrowserTheme();
}

/**
 * The web client deliberately blocks inline styles in its CSP. Pierre emits
 * generated grid placement and token styles as style attributes/elements, so
 * re-apply them through the CSSOM instead of weakening the page policy.
 */
function applyPierreDynamicStyles(container: HTMLElement): void {
	const root = container.shadowRoot;
	if (!root) return;
	for (const element of Array.from(
		root.querySelectorAll<HTMLElement>("[style]"),
	)) {
		const cssText = element.getAttribute("style");
		if (!cssText) continue;
		// Parsing the original attribute happens under CSP and leaves the CSSOM
		// declaration empty. Reassigning through the DOM API applies it safely.
		element.style.cssText = "";
		element.style.cssText = cssText;
	}
	const generatedSheets: CSSStyleSheet[] = [];
	for (const style of Array.from(root.querySelectorAll("style"))) {
		const sheet = new CSSStyleSheet();
		sheet.replaceSync(style.textContent ?? "");
		generatedSheets.push(sheet);
	}
	const coreSheet = root.adoptedStyleSheets[0];
	root.adoptedStyleSheets = [
		...(coreSheet ? [coreSheet] : []),
		...generatedSheets,
	];
}

function findFileDiff(file: RemoteReviewFile): FileDiffMetadata {
	const parsed = parsePatchFiles(file.file.rawPatch, file.file.id);
	const files = parsed.flatMap((patch) => patch.files);
	const match = files.find(
		(candidate) =>
			candidate.name === file.file.path ||
			candidate.prevName === file.file.prevPath,
	);
	if (!match) throw new Error("Could not render this file diff");
	return match;
}

export function PierreDiff(props: {
	file: RemoteReviewFile;
	layout: ReviewDiffLayout;
	overflow: ReviewDiffOverflow;
}): JSX.Element {
	let host: HTMLDivElement | undefined;

	createEffect(() => {
		const file = props.file;
		const layout = props.layout;
		const overflow = props.overflow;
		if (!host) return;
		host.replaceChildren();
		const renderer = new FileDiff({
			disableFileHeader: true,
			disableVirtualizationBuffers: true,
			diffStyle: layout,
			overflow,
			themeType: themeType(),
		});
		let observer: MutationObserver | undefined;
		try {
			renderer.render({
				fileDiff: findFileDiff(file),
				containerWrapper: host,
			});
			const container = host.querySelector<HTMLElement>("diffs-container");
			if (container) {
				applyPierreDynamicStyles(container);
				const root = container.shadowRoot;
				if (root) {
					let queued = false;
					observer = new MutationObserver(() => {
						if (queued) return;
						queued = true;
						queueMicrotask(() => {
							queued = false;
							applyPierreDynamicStyles(container);
						});
					});
					observer.observe(root, {
						childList: true,
						characterData: true,
						subtree: true,
					});
				}
			}
		} catch (cause) {
			host.textContent = cause instanceof Error ? cause.message : String(cause);
		}
		onCleanup(() => {
			observer?.disconnect();
			renderer.cleanUp();
		});
	});

	return <div ref={host} class="pierre-diff-host" />;
}
