import type { BoxProps } from "@opentui/solid";
import type { JSX } from "solid-js";

const CUSTOM_OVERLAY_Z_FLOOR = 1300;
const CUSTOM_OVERLAY_Z_STEP = 10;

export type OverlaySurfaceProps = Pick<BoxProps, "zIndex">;

export type OverlayComponentProps<T> = {
	done: (result: T) => void;
	surfaceProps: OverlaySurfaceProps;
	active: boolean;
	copyText: (text: string) => Promise<void>;
};

export type OverlayEntry = {
	id: string;
	component: (props: OverlayComponentProps<unknown>) => JSX.Element;
	resolve: (result: unknown) => void;
};

export function getOverlaySurfaceProps(index: number): OverlaySurfaceProps {
	return { zIndex: CUSTOM_OVERLAY_Z_FLOOR + index * CUSTOM_OVERLAY_Z_STEP };
}

export function getToastStackZIndex(overlayCount: number): number {
	return CUSTOM_OVERLAY_Z_FLOOR + overlayCount * CUSTOM_OVERLAY_Z_STEP;
}

export function createCustomOverlayHandler(
	setOverlays: (fn: (prev: OverlayEntry[]) => OverlayEntry[]) => void,
): <T>(
	component: (props: OverlayComponentProps<T>) => JSX.Element,
) => Promise<T> {
	return <T>(
		component: (props: OverlayComponentProps<T>) => JSX.Element,
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
