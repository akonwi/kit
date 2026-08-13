/** @jsxImportSource solid-js */
import type { JSX } from "solid-js";

export type WebIconName = "attach" | "close" | "command" | "send" | "stop";

export function WebIcon(props: { name: WebIconName }): JSX.Element {
	return (
		<svg
			class="web-icon"
			viewBox="0 0 24 24"
			fill="none"
			stroke="currentColor"
			stroke-width="2"
			stroke-linecap="round"
			stroke-linejoin="round"
			aria-hidden="true"
		>
			{props.name === "attach" ? (
				<path d="M20.5 11.5 11 21a6 6 0 0 1-8.5-8.5l10-10a4 4 0 0 1 5.7 5.7l-10 10a2 2 0 0 1-2.9-2.8l9.3-9.3" />
			) : props.name === "command" ? (
				<>
					<rect x="3" y="5" width="18" height="14" rx="1" />
					<path d="m7 9 3 3-3 3M13 15h4" />
				</>
			) : props.name === "send" ? (
				<path d="M12 19V5m-6 6 6-6 6 6" />
			) : props.name === "stop" ? (
				<rect x="7" y="7" width="10" height="10" />
			) : (
				<path d="m6 6 12 12M18 6 6 18" />
			)}
		</svg>
	);
}
