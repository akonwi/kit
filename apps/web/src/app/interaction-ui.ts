import type { JSX } from "solid-js";
import type {
	InternalPluginInteractionComponentProps,
	InternalPluginInteractionOptions,
	OpenPluginInteraction,
} from "../plugins/types";

export type InteractionComponentProps<T> =
	InternalPluginInteractionComponentProps<T>;
export type InteractionOpenOptions<T> = InternalPluginInteractionOptions<T>;

export type InteractionEntry = {
	id: string;
	component: (props: InteractionComponentProps<unknown>) => JSX.Element;
	resolve: (result: unknown) => void;
	cancel: () => void;
};

export type OpenInteraction = OpenPluginInteraction;

export function createInteractionHandler(
	getInteractions: () => InteractionEntry[],
	setInteractions: (interactions: InteractionEntry[]) => void,
): OpenInteraction {
	return <T>(
		component: (props: InteractionComponentProps<T>) => JSX.Element,
		options: InteractionOpenOptions<T>,
	): Promise<T> => {
		return new Promise<T>((resolve) => {
			const id = crypto.randomUUID();
			let settled = false;
			const abort = () => settle(options.abortValue);
			const settle = (result: T) => {
				if (settled) return;
				settled = true;
				options.signal?.removeEventListener("abort", abort);
				setInteractions(getInteractions().filter((entry) => entry.id !== id));
				resolve(result);
			};
			const entry: InteractionEntry = {
				id,
				component: component as InteractionEntry["component"],
				resolve: (result) => settle(result as T),
				cancel: () => settle(options.abortValue),
			};
			if (options.signal?.aborted) {
				settle(options.abortValue);
				return;
			}
			options.signal?.addEventListener("abort", abort, { once: true });
			setInteractions([...getInteractions(), entry]);
		});
	};
}
