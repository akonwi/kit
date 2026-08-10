export type BrowserTheme = "system" | "light" | "dark";

export const BROWSER_THEME_STORAGE_KEY = "kit.web.theme";

export const BROWSER_THEME_OPTIONS: readonly {
	id: BrowserTheme;
	name: string;
	description: string;
}[] = [
	{
		id: "system",
		name: "System",
		description: "Follow browser and operating system settings",
	},
	{
		id: "light",
		name: "Light",
		description: "Always use the light appearance",
	},
	{
		id: "dark",
		name: "Dark",
		description: "Always use the dark appearance",
	},
];

type ThemeStyle = Pick<CSSStyleDeclaration, "removeProperty" | "setProperty">;

type ThemeRoot = {
	style: ThemeStyle;
};

export function isBrowserTheme(value: unknown): value is BrowserTheme {
	return value === "system" || value === "light" || value === "dark";
}

export function readBrowserTheme(storage?: Storage): BrowserTheme {
	try {
		const target = storage ?? window.localStorage;
		const value = target.getItem(BROWSER_THEME_STORAGE_KEY);
		return isBrowserTheme(value) ? value : "system";
	} catch {
		return "system";
	}
}

export function applyBrowserTheme(
	theme: BrowserTheme,
	root: ThemeRoot = document.documentElement,
): void {
	if (theme === "system") {
		root.style.removeProperty("color-scheme");
		root.style.removeProperty("--check-glyph");
		return;
	}
	root.style.setProperty("color-scheme", theme);
	root.style.setProperty("--check-glyph", `var(--check-glyph-${theme})`);
}

export function persistBrowserTheme(
	theme: BrowserTheme,
	storage?: Storage,
): void {
	try {
		const target = storage ?? window.localStorage;
		target.setItem(BROWSER_THEME_STORAGE_KEY, theme);
	} catch {
		// The selected theme still applies for this page when storage is unavailable.
	}
}

export function initializeBrowserTheme(): BrowserTheme {
	const theme = readBrowserTheme();
	applyBrowserTheme(theme);
	return theme;
}
