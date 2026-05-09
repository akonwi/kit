import { createSignal, onCleanup } from "solid-js";
import { SPINNER_FRAMES } from "./glyphs";

const SPINNER_INTERVAL = 80;

export type SpinnerProps = {
	fg: string;
};

export function Spinner(props: SpinnerProps) {
	const [frame, setFrame] = createSignal(0);
	const timer = setInterval(() => {
		setFrame((f) => (f + 1) % SPINNER_FRAMES.length);
	}, SPINNER_INTERVAL);
	onCleanup(() => clearInterval(timer));

	return <text fg={props.fg}>{SPINNER_FRAMES[frame()]}</text>;
}
