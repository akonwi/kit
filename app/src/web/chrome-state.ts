import type { ChromeThemeToken } from "../shell/chrome-contributions";

export type RemoteChromeTextStyle = {
	fgToken?: ChromeThemeToken;
	bgToken?: ChromeThemeToken;
	bold?: boolean;
	dim?: boolean;
	italic?: boolean;
	underline?: boolean;
	strikethrough?: boolean;
};

export type RemoteChromeSegment = {
	text: string;
	style?: RemoteChromeTextStyle;
};

export type RemoteChromeContribution = {
	id: string;
	content: RemoteChromeSegment[];
	plainText: string;
	side: "left" | "right";
	clickable: boolean;
};

export type RemoteChromeArea = {
	contributions: RemoteChromeContribution[];
	hiddenBuiltinIds: string[];
};

export type RemoteChromeSnapshot = {
	header: RemoteChromeArea;
	footer: RemoteChromeArea;
};

export const EMPTY_REMOTE_CHROME: RemoteChromeSnapshot = {
	header: { contributions: [], hiddenBuiltinIds: [] },
	footer: { contributions: [], hiddenBuiltinIds: [] },
};

export const CHROME_TOKEN_COLORS: Record<ChromeThemeToken, string> = {
	bg: "var(--kit-bg)",
	bgSurface: "var(--kit-bg-surface)",
	bgMuted: "var(--neutral-3)",
	bgAccent: "var(--accent-3)",
	bgTransparent: "transparent",
	borderDefault: "var(--kit-border-default)",
	borderFocused: "var(--kit-border-focused)",
	borderAccent: "var(--kit-border-accent)",
	borderDebug: "var(--accent-7)",
	borderStatus: "var(--kit-border-default)",
	composerBashBorder: "var(--kit-tool-text)",
	composerBashExcludedBorder: "var(--kit-text-muted)",
	textPrimary: "var(--kit-text-primary)",
	textSecondary: "var(--kit-text-secondary)",
	textMuted: "var(--kit-text-muted)",
	textPlaceholder: "var(--kit-text-placeholder)",
	textDebug: "var(--accent-11)",
	userText: "var(--kit-border-accent)",
	userTextFocused: "var(--accent-10)",
	userBorder: "var(--kit-user-border)",
	assistantText: "var(--kit-text-primary)",
	toolText: "var(--kit-tool-text)",
	reviewText: "var(--kit-attachment-text)",
	errorText: "var(--kit-error-text)",
	warningText: "var(--color-warn-text)",
	subagentText: "var(--accent-11)",
	debugLabel: "var(--accent-11)",
	metaText: "var(--kit-text-secondary)",
	attachmentText: "var(--kit-attachment-text)",
	cursor: "var(--kit-border-accent)",
	pickerBg: "var(--kit-bg-surface)",
	pickerBorder: "var(--kit-border-default)",
	pickerFocusedBg: "var(--kit-picker-focused-bg)",
	pickerFocusedText: "var(--kit-picker-focused-text)",
	pickerItemText: "var(--kit-text-primary)",
	pickerScrollThumb: "var(--neutral-8)",
	pickerScrollTrack: "var(--neutral-3)",
	scrollbarFg: "var(--neutral-8)",
	scrollbarBg: "var(--neutral-3)",
	panelText: "var(--kit-panel-text)",
	progressNormal: "var(--kit-border-accent)",
	progressWarning: "var(--color-warn-text)",
	progressCritical: "var(--kit-error-text)",
	toggleOn: "var(--kit-tool-text)",
	diffAddedBg: "var(--color-success-surface)",
	diffRemovedBg: "var(--color-danger-surface)",
	diffAddedContentBg: "var(--color-success-surface)",
	diffRemovedContentBg: "var(--color-danger-surface)",
	diffAddedLineNumberBg: "var(--color-success-border)",
	diffRemovedLineNumberBg: "var(--color-danger-border)",
	diffCursorBg: "var(--accent-3)",
	diffCursorGutterBg: "var(--accent-5)",
	diffCursorAddedBg: "var(--color-success-border)",
	diffCursorRemovedBg: "var(--color-danger-border)",
};

const CHROME_THEME_TOKEN_SET = new Set<string>(
	Object.keys(CHROME_TOKEN_COLORS),
);
const MAX_CONTRIBUTIONS = 64;
const MAX_SEGMENTS = 32;
const MAX_TOTAL_TEXT_LENGTH = 8_192;
const MAX_ID_LENGTH = 256;

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasControlCharacter(value: string): boolean {
	for (let index = 0; index < value.length; index += 1) {
		const code = value.charCodeAt(index);
		if (code < 32 || code === 127) return true;
	}
	return false;
}

function parseStyle(value: unknown): RemoteChromeTextStyle | undefined | null {
	if (value === undefined) return undefined;
	if (!isRecord(value)) return null;
	const style: RemoteChromeTextStyle = {};
	for (const key of ["fgToken", "bgToken"] as const) {
		const token = value[key];
		if (
			token !== undefined &&
			(typeof token !== "string" || !CHROME_THEME_TOKEN_SET.has(token))
		) {
			return null;
		}
		if (typeof token === "string") style[key] = token as ChromeThemeToken;
	}
	for (const key of [
		"bold",
		"dim",
		"italic",
		"underline",
		"strikethrough",
	] as const) {
		const enabled = value[key];
		if (enabled !== undefined && typeof enabled !== "boolean") return null;
		if (enabled === true) style[key] = true;
	}
	return Object.keys(style).length > 0 ? style : undefined;
}

function parseArea(value: unknown): RemoteChromeArea | null {
	if (
		!isRecord(value) ||
		!Array.isArray(value.contributions) ||
		value.contributions.length > MAX_CONTRIBUTIONS ||
		!Array.isArray(value.hiddenBuiltinIds)
	) {
		return null;
	}
	const ids = new Set<string>();
	let totalTextLength = 0;
	const contributions: RemoteChromeContribution[] = [];
	for (const raw of value.contributions) {
		if (
			!isRecord(raw) ||
			typeof raw.id !== "string" ||
			raw.id.length === 0 ||
			raw.id.length > MAX_ID_LENGTH ||
			hasControlCharacter(raw.id) ||
			ids.has(raw.id) ||
			!Array.isArray(raw.content) ||
			raw.content.length > MAX_SEGMENTS ||
			typeof raw.plainText !== "string" ||
			(raw.side !== "left" && raw.side !== "right") ||
			typeof raw.clickable !== "boolean"
		) {
			return null;
		}
		ids.add(raw.id);
		const content: RemoteChromeSegment[] = [];
		for (const rawSegment of raw.content) {
			if (
				!isRecord(rawSegment) ||
				typeof rawSegment.text !== "string" ||
				hasControlCharacter(rawSegment.text)
			) {
				return null;
			}
			const style = parseStyle(rawSegment.style);
			if (style === null) return null;
			totalTextLength += rawSegment.text.length;
			if (totalTextLength > MAX_TOTAL_TEXT_LENGTH) return null;
			content.push({
				text: rawSegment.text,
				...(style ? { style } : {}),
			});
		}
		if (content.map((segment) => segment.text).join("") !== raw.plainText) {
			return null;
		}
		contributions.push({
			id: raw.id,
			content,
			plainText: raw.plainText,
			side: raw.side,
			clickable: raw.clickable,
		});
	}
	const hiddenBuiltinIds: string[] = [];
	for (const id of value.hiddenBuiltinIds) {
		if (
			typeof id !== "string" ||
			id.length === 0 ||
			id.length > MAX_ID_LENGTH ||
			hasControlCharacter(id) ||
			hiddenBuiltinIds.includes(id)
		) {
			return null;
		}
		hiddenBuiltinIds.push(id);
	}
	return { contributions, hiddenBuiltinIds };
}

export function parseRemoteChromeSnapshot(
	value: unknown,
): RemoteChromeSnapshot | null {
	if (!isRecord(value)) return null;
	const header = parseArea(value.header);
	const footer = parseArea(value.footer);
	return header && footer ? { header, footer } : null;
}
