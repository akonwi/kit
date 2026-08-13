import { describe, expect, test } from "bun:test";
import {
	applyBrowserTheme,
	BROWSER_THEME_STORAGE_KEY,
	persistBrowserTheme,
	readBrowserTheme,
} from "./browser-theme";

function createStorage(initial?: Record<string, string>): Storage {
	const values = new Map(Object.entries(initial ?? {}));
	return {
		get length() {
			return values.size;
		},
		clear: () => values.clear(),
		getItem: (key) => values.get(key) ?? null,
		key: (index) => [...values.keys()][index] ?? null,
		removeItem: (key) => values.delete(key),
		setItem: (key, value) => values.set(key, value),
	};
}

function createRoot() {
	const properties = new Map<string, string>();
	return {
		properties,
		root: {
			style: {
				removeProperty: (name: string) => {
					const previous = properties.get(name) ?? "";
					properties.delete(name);
					return previous;
				},
				setProperty: (name: string, value: string) => {
					properties.set(name, value);
				},
			},
		},
	};
}

describe("browser theme", () => {
	test("applies forced light and dark color schemes", () => {
		const { properties, root } = createRoot();

		applyBrowserTheme("dark", root);
		expect(Object.fromEntries(properties)).toEqual({
			"color-scheme": "dark",
			"--check-glyph": "var(--check-glyph-dark)",
		});

		applyBrowserTheme("light", root);
		expect(Object.fromEntries(properties)).toEqual({
			"color-scheme": "light",
			"--check-glyph": "var(--check-glyph-light)",
		});
	});

	test("restores stylesheet-driven system appearance", () => {
		const { properties, root } = createRoot();
		applyBrowserTheme("dark", root);
		applyBrowserTheme("system", root);
		expect(properties.size).toBe(0);
	});

	test("persists valid choices and ignores invalid stored values", () => {
		const storage = createStorage();
		persistBrowserTheme("light", storage);
		expect(readBrowserTheme(storage)).toBe("light");

		storage.setItem(BROWSER_THEME_STORAGE_KEY, "custom");
		expect(readBrowserTheme(storage)).toBe("system");
	});

	test("falls back when browser storage is restricted", () => {
		const restricted = createStorage();
		restricted.getItem = () => {
			throw new DOMException("blocked", "SecurityError");
		};
		restricted.setItem = () => {
			throw new DOMException("blocked", "SecurityError");
		};

		expect(readBrowserTheme(restricted)).toBe("system");
		expect(() => persistBrowserTheme("dark", restricted)).not.toThrow();
	});
});
