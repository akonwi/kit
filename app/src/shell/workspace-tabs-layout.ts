import { terminalTextWidth, truncateEnd } from "./chrome-layout";

export type WorkspaceTabItem = {
	id: string;
	label: string;
	closable?: boolean;
};

export type WorkspaceVisibleTab = WorkspaceTabItem & {
	displayLabel: string;
	width: number;
};

export type WideWorkspaceTabLayout = {
	visible: readonly WorkspaceVisibleTab[];
	hiddenBefore: number;
	hiddenAfter: number;
};

const TAB_HORIZONTAL_CHROME = 4;
const MIN_TAB_WIDTH = 6;
const MAX_TAB_WIDTH = 24;
const COLLAPSE_WIDTH = 2;

function indicatorWidth(count: number): number {
	return count > 0 ? String(count).length + 2 : 0;
}

function visibleTab(
	tab: WorkspaceTabItem,
	maxWidth = MAX_TAB_WIDTH,
	minWidth = MIN_TAB_WIDTH,
) {
	const horizontalChrome = tab.closable === false ? 2 : TAB_HORIZONTAL_CHROME;
	const width = Math.max(
		minWidth,
		Math.min(maxWidth, terminalTextWidth(tab.label) + horizontalChrome),
	);
	return {
		...tab,
		displayLabel: truncateEnd(tab.label, Math.max(1, width - horizontalChrome)),
		width,
	};
}

export function resolveWideWorkspaceTabs(options: {
	tabs: readonly WorkspaceTabItem[];
	width: number;
	offset: number;
}): WideWorkspaceTabLayout {
	const offset = Math.max(0, Math.min(options.offset, options.tabs.length - 1));
	const hiddenBefore = offset;
	let hiddenAfter = 0;
	let visible: WorkspaceVisibleTab[] = [];

	for (let pass = 0; pass < 2; pass += 1) {
		const available = Math.max(
			MIN_TAB_WIDTH,
			options.width -
				COLLAPSE_WIDTH -
				indicatorWidth(hiddenBefore) -
				indicatorWidth(hiddenAfter),
		);
		visible = [];
		let used = 0;
		for (const tab of options.tabs.slice(offset)) {
			const item = visibleTab(tab, available);
			if (visible.length > 0 && used + item.width > available) break;
			visible.push(item);
			used += item.width;
		}
		hiddenAfter = Math.max(0, options.tabs.length - offset - visible.length);
	}

	return { visible, hiddenBefore, hiddenAfter };
}

export function revealWideWorkspaceTab(options: {
	tabs: readonly WorkspaceTabItem[];
	width: number;
	offset: number;
	activeTabId: string;
}): number {
	const activeIndex = options.tabs.findIndex(
		(tab) => tab.id === options.activeTabId,
	);
	if (activeIndex < 0) return options.offset;
	let offset = Math.max(0, Math.min(options.offset, activeIndex));
	while (offset < activeIndex) {
		const layout = resolveWideWorkspaceTabs({
			tabs: options.tabs,
			width: options.width,
			offset,
		});
		if (layout.visible.some((tab) => tab.id === options.activeTabId)) break;
		offset += 1;
	}
	return offset;
}

export function resolveNarrowWorkspaceTabs(options: {
	tabs: readonly WorkspaceTabItem[];
	width: number;
}): {
	visible: readonly WorkspaceVisibleTab[];
	overflow: readonly WorkspaceTabItem[];
} {
	const fullTranscript = visibleTab({
		id: "transcript",
		label: "Transcript",
		closable: false,
	});
	const workspaceTabs = options.tabs.map((tab) =>
		visibleTab(tab, Math.max(8, options.width)),
	);
	const all = [fullTranscript, ...workspaceTabs];
	let visible = all;
	let overflow: WorkspaceTabItem[] = [];
	const totalWidth = all.reduce((sum, tab) => sum + tab.width, 0);
	if (totalWidth > options.width) {
		const overflowWidth = String(Math.max(1, options.tabs.length)).length + 2;
		const transcriptWidth = Math.max(1, options.width - overflowWidth);
		const transcript = visibleTab(
			{ id: "transcript", label: "Transcript", closable: false },
			transcriptWidth,
			Math.min(MIN_TAB_WIDTH, transcriptWidth),
		);
		visible = [transcript];
		for (const tab of workspaceTabs) {
			const remaining = all.length - visible.length;
			const overflowWidth = String(Math.max(1, remaining)).length + 2;
			const used = visible.reduce((sum, item) => sum + item.width, 0);
			if (used + tab.width + overflowWidth > options.width) break;
			visible.push(tab);
		}
		const visibleIds = new Set(visible.map((tab) => tab.id));
		overflow = options.tabs.filter((tab) => !visibleIds.has(tab.id));
	}
	return { visible, overflow };
}
