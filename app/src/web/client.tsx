/** @jsxImportSource solid-js */
import { render } from "solid-js/web";
import { App } from "./App";
import { showMicaToast } from "./mica-toasts";
import { WebClientProvider } from "./WebClientContext";

const root = document.querySelector<HTMLElement>("#app");
if (!root) throw new Error("Missing web client mount point");

render(
	() => (
		<WebClientProvider showToast={showMicaToast}>
			<App />
		</WebClientProvider>
	),
	root,
);
