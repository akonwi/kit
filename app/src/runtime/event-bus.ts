/**
 * Typed event bus with wildcard, exact-match, and prefix subscriptions.
 *
 * Events are discriminated unions keyed by dotted string names.
 * The bus constructs `{ type, ...payload }` internally on publish.
 */

/**
 * An event map: keys are event names, values are payload shapes.
 * Each payload must be a plain object (spread into the final event).
 */
// biome-ignore lint/suspicious/noExplicitAny: event map values are heterogeneous payload shapes
export type EventMap = Record<string, Record<string, any>>;

/** A fully constructed event: `{ type: K } & Payload`. */
export type Event<M extends EventMap, K extends keyof M & string> = {
	type: K;
} & M[K];

/** Union of all possible events for a given map. */
export type AnyEvent<M extends EventMap> = {
	[K in keyof M & string]: Event<M, K>;
}[keyof M & string];

/** Event names that start with a given prefix. */
export type KeysMatchingPrefix<M extends EventMap, P extends string> = Extract<
	keyof M & string,
	`${P}${string}`
>;

export type PrefixSubscription<P extends string> = { prefix: P };

export class EventBus<M extends EventMap> {
	private readonly wildcardListeners = new Set<(event: AnyEvent<M>) => void>();
	private readonly exactListeners = new Map<
		string,
		Set<(event: AnyEvent<M>) => void>
	>();
	private readonly prefixListeners: Array<{
		prefix: string;
		listener: (event: AnyEvent<M>) => void;
	}> = [];

	/** Subscribe to all events. */
	subscribe(listener: (event: AnyEvent<M>) => void): () => void;
	/** Subscribe to a single event type with narrowed typing. */
	subscribe<K extends keyof M & string>(
		type: K,
		listener: (event: Event<M, K>) => void,
	): () => void;
	/** Subscribe to all events matching a dotted prefix. */
	subscribe<P extends string>(
		options: PrefixSubscription<P>,
		listener: (event: Event<M, KeysMatchingPrefix<M, P>>) => void,
	): () => void;
	subscribe(
		typeOrListener:
			| string
			| PrefixSubscription<string>
			| ((event: AnyEvent<M>) => void),
		// biome-ignore lint/suspicious/noExplicitAny: implementation signature must accept all overload listener types
		maybeListener?: (event: any) => void,
	): () => void {
		if (typeof typeOrListener === "function") {
			const listener = typeOrListener;
			this.wildcardListeners.add(listener);
			return () => this.wildcardListeners.delete(listener);
		}

		const listener = maybeListener as (event: AnyEvent<M>) => void;

		if (typeof typeOrListener === "object" && typeOrListener !== null) {
			const entry = { prefix: typeOrListener.prefix, listener };
			this.prefixListeners.push(entry);
			return () => {
				const index = this.prefixListeners.indexOf(entry);
				if (index >= 0) this.prefixListeners.splice(index, 1);
			};
		}

		const type = typeOrListener;
		const listeners = this.exactListeners.get(type) ?? new Set();
		listeners.add(listener);
		this.exactListeners.set(type, listeners);
		return () => {
			const current = this.exactListeners.get(type);
			if (!current) return;
			current.delete(listener);
			if (current.size === 0) this.exactListeners.delete(type);
		};
	}

	/** Publish a typed event. Constructs `{ type, ...payload }` internally. */
	publish<K extends keyof M & string>(type: K, payload: M[K]): void {
		const event = { type, ...payload } as AnyEvent<M>;
		for (const listener of this.wildcardListeners) listener(event);
		const exact = this.exactListeners.get(type);
		if (exact) {
			for (const listener of exact) listener(event);
		}
		for (const { prefix, listener } of this.prefixListeners) {
			if (type.startsWith(prefix)) listener(event);
		}
	}

	/** Remove all listeners. */
	dispose(): void {
		this.wildcardListeners.clear();
		this.exactListeners.clear();
		this.prefixListeners.length = 0;
	}
}
