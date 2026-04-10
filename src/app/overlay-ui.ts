import type { JSX } from "solid-js";

export type OverlayEntry = {
	id: string;
	component: (props: { done: (result: unknown) => void }) => JSX.Element;
	resolve: (result: unknown) => void;
};

export function createCustomOverlayHandler(
	setOverlays: (fn: (prev: OverlayEntry[]) => OverlayEntry[]) => void,
): <T>(
	component: (props: { done: (result: T) => void }) => JSX.Element,
) => Promise<T> {
	return <T>(
		component: (props: { done: (result: T) => void }) => JSX.Element,
	): Promise<T> => {
		return new Promise<T>((resolve) => {
			const id = crypto.randomUUID();
			const entry: OverlayEntry = {
				id,
				component: component as OverlayEntry["component"],
				resolve: (result) => {
					setOverlays((prev) => prev.filter((e) => e.id !== id));
					resolve(result as T);
				},
			};
			setOverlays((prev) => [...prev, entry]);
		});
	};
}
