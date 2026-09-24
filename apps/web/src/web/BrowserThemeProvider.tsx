/** @jsxImportSource solid-js */
import { createContext, createSignal, type JSX, useContext } from "solid-js";
import { Portal } from "solid-js/web";
import {
	applyBrowserTheme,
	BROWSER_THEME_OPTIONS,
	type BrowserTheme,
	persistBrowserTheme,
	readBrowserTheme,
} from "./browser-theme";
import { PickerDialog, type PickerDialogOption } from "./PickerDialog";

type BrowserThemeContextValue = {
	openThemePicker(): void;
};

const BrowserThemeContext = createContext<BrowserThemeContextValue>();

export function useBrowserTheme(): BrowserThemeContextValue {
	const value = useContext(BrowserThemeContext);
	if (!value) {
		throw new Error("useBrowserTheme must be used within BrowserThemeProvider");
	}
	return value;
}

const THEME_OPTIONS: readonly PickerDialogOption<BrowserTheme>[] =
	BROWSER_THEME_OPTIONS.map((theme) => ({
		id: theme.id,
		name: theme.name,
		description: theme.description,
		value: theme.id,
	}));

export function BrowserThemeProvider(props: {
	children: JSX.Element;
}): JSX.Element {
	const [theme, setTheme] = createSignal(readBrowserTheme());
	const [pickerOpen, setPickerOpen] = createSignal(false);

	function openThemePicker(): void {
		if (document.querySelector<HTMLDialogElement>("dialog:modal")) return;
		setPickerOpen(true);
	}

	function closeThemePicker(): void {
		setPickerOpen(false);
	}

	function selectTheme(next: BrowserTheme): void {
		applyBrowserTheme(next);
		persistBrowserTheme(next);
		setTheme(next);
		closeThemePicker();
	}

	return (
		<BrowserThemeContext.Provider value={{ openThemePicker }}>
			{props.children}
			<Portal>
				<PickerDialog
					open={pickerOpen()}
					id="theme-picker"
					title="Theme"
					options={THEME_OPTIONS}
					currentId={theme()}
					filter="none"
					layout="detail"
					onCancel={closeThemePicker}
					onSelect={selectTheme}
				/>
			</Portal>
		</BrowserThemeContext.Provider>
	);
}
