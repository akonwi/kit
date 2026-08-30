import { normalizeRemoteHttpUrl } from "./remote-url";

export type RemoteChromeTextStyle = {
	fgToken?: RemoteChromeThemeToken;
	bgToken?: RemoteChromeThemeToken;
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
	action?: { type: "open-url"; url: string };
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

export const REMOTE_CHROME_THEME_TOKENS = [
	"bg",
	"bgSurface",
	"bgMuted",
	"bgAccent",
	"bgTransparent",
	"borderDefault",
	"borderFocused",
	"borderAccent",
	"borderDebug",
	"borderStatus",
	"composerBashBorder",
	"composerBashExcludedBorder",
	"textPrimary",
	"textSecondary",
	"textMuted",
	"textPlaceholder",
	"textDebug",
	"userText",
	"userTextFocused",
	"userBorder",
	"assistantText",
	"toolText",
	"reviewText",
	"errorText",
	"warningText",
	"subagentText",
	"debugLabel",
	"metaText",
	"attachmentText",
	"cursor",
	"pickerBg",
	"pickerBorder",
	"pickerFocusedBg",
	"pickerFocusedText",
	"pickerItemText",
	"pickerScrollThumb",
	"pickerScrollTrack",
	"scrollbarFg",
	"scrollbarBg",
	"panelText",
	"progressNormal",
	"progressWarning",
	"progressCritical",
	"toggleOn",
	"diffAddedBg",
	"diffRemovedBg",
	"diffAddedContentBg",
	"diffRemovedContentBg",
	"diffAddedLineNumberBg",
	"diffRemovedLineNumberBg",
	"diffCursorBg",
	"diffCursorGutterBg",
	"diffCursorAddedBg",
	"diffCursorRemovedBg",
] as const;

export type RemoteChromeThemeToken =
	(typeof REMOTE_CHROME_THEME_TOKENS)[number];

const CHROME_THEME_TOKEN_SET = new Set<string>(REMOTE_CHROME_THEME_TOKENS);
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

function parseAction(
	value: unknown,
): RemoteChromeContribution["action"] | undefined | null {
	if (value === undefined) return undefined;
	if (!isRecord(value) || value.type !== "open-url") return null;
	const url = normalizeRemoteHttpUrl(value.url);
	return url ? { type: "open-url", url } : null;
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
		if (typeof token === "string") style[key] = token as RemoteChromeThemeToken;
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
		const action = parseAction(raw.action);
		if (action === null || (action && raw.clickable)) return null;
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
			...(action ? { action } : {}),
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
