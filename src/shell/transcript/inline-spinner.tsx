import { createSignal, onCleanup } from "solid-js";
import { SPINNER_FRAMES } from "../glyphs";
import { theme } from "../theme";

const SPINNER_INTERVAL = 80;

export function InlineSpinner() {
	const [frame, setFrame] = createSignal(0);
	const timer = setInterval(() => {
		setFrame((f) => (f + 1) % SPINNER_FRAMES.length);
	}, SPINNER_INTERVAL);
	onCleanup(() => clearInterval(timer));
	return <text fg={theme.toolText}>{SPINNER_FRAMES[frame()]}</text>;
}
