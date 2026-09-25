export type BrowserTheme = {
	background: string;
	foreground: string;
	cursor: string;
	selectionBackground: string;
	statusBackground: string;
	statusForeground: string;
	statusBorder: string;
};

export type BrowserThemeMessage = {
	type: "theme";
	theme: BrowserTheme;
};

const HEX_COLOR = /^#[\da-f]{6}$/i;
const THEME_KEYS = [
	"background",
	"foreground",
	"cursor",
	"selectionBackground",
	"statusBackground",
	"statusForeground",
	"statusBorder",
] as const;

export function parseBrowserThemeMessage(value: string): BrowserTheme | null {
	if (value.length > 1024) return null;
	let parsed: unknown;
	try {
		parsed = JSON.parse(value);
	} catch {
		return null;
	}
	if (typeof parsed !== "object" || parsed === null) return null;
	const message = parsed as Record<string, unknown>;
	if (message.type !== "theme") return null;
	if (typeof message.theme !== "object" || message.theme === null) return null;
	const theme = message.theme as Record<string, unknown>;
	for (const key of THEME_KEYS) {
		if (typeof theme[key] !== "string" || !HEX_COLOR.test(theme[key])) {
			return null;
		}
	}
	return theme as BrowserTheme;
}

export function colorSchemeForBackground(background: string): "dark" | "light" {
	const red = Number.parseInt(background.slice(1, 3), 16);
	const green = Number.parseInt(background.slice(3, 5), 16);
	const blue = Number.parseInt(background.slice(5, 7), 16);
	return (red * 299 + green * 587 + blue * 114) / 1000 < 128 ? "dark" : "light";
}

export function applyBrowserTheme(
	theme: BrowserTheme,
	root: HTMLElement = document.documentElement,
): void {
	root.style.setProperty("--kit-terminal-bg", theme.background);
	root.style.setProperty("--kit-terminal-fg", theme.foreground);
	root.style.setProperty("--kit-terminal-cursor", theme.cursor);
	root.style.setProperty(
		"--kit-terminal-selection-bg",
		theme.selectionBackground,
	);
	root.style.setProperty("--kit-status-bg", theme.statusBackground);
	root.style.setProperty("--kit-status-fg", theme.statusForeground);
	root.style.setProperty("--kit-status-border", theme.statusBorder);
	root.style.colorScheme = colorSchemeForBackground(theme.background);
}
