/** @jsxImportSource solid-js */
import {
	type Accessor,
	createContext,
	createEffect,
	createSignal,
	type JSX,
	onCleanup,
	useContext,
} from "solid-js";
import type { RemoteReviewFile, RemoteReviewState } from "./remote-services";
import { useWebClient } from "./WebClientContext";

type CodeReviewContextValue = {
	open: Accessor<boolean>;
	loading: Accessor<boolean>;
	error: Accessor<string>;
	state: Accessor<RemoteReviewState | null>;
	selectedFile: Accessor<RemoteReviewFile | null>;
	openReview(): void;
	close(): void;
	selectFile(path: string): Promise<void>;
	refresh(): Promise<void>;
};

const CodeReviewContext = createContext<CodeReviewContextValue>();

export function useCodeReview(): CodeReviewContextValue {
	const value = useContext(CodeReviewContext);
	if (!value) throw new Error("CodeReviewProvider is missing");
	return value;
}

export function CodeReviewProvider(props: {
	children: JSX.Element;
}): JSX.Element {
	const { controller, snapshot } = useWebClient();
	const [open, setOpen] = createSignal(false);
	const [loading, setLoading] = createSignal(false);
	const [error, setError] = createSignal("");
	const [state, setState] = createSignal<RemoteReviewState | null>(null);
	const [selectedFile, setSelectedFile] = createSignal<RemoteReviewFile | null>(
		null,
	);
	let generation = 0;
	let observedSessionId: unknown;

	async function loadFile(path: string, loadGeneration: number): Promise<void> {
		const file = await controller.getReviewFile(path);
		if (loadGeneration !== generation || !open()) return;
		setSelectedFile(file);
	}

	async function refresh(): Promise<void> {
		if (!open()) return;
		const loadGeneration = ++generation;
		setLoading(true);
		setError("");
		try {
			const next = await controller.getReviewState();
			if (loadGeneration !== generation || !open()) return;
			setState(next);
			const currentPath = selectedFile()?.file.path;
			const path = next.files.some((file) => file.path === currentPath)
				? currentPath
				: next.files[0]?.path;
			if (path) await loadFile(path, loadGeneration);
			else setSelectedFile(null);
		} catch (cause) {
			if (loadGeneration !== generation) return;
			setError(cause instanceof Error ? cause.message : String(cause));
		} finally {
			if (loadGeneration === generation) setLoading(false);
		}
	}

	function openReview(): void {
		if (snapshot().protocol.phase !== "live") return;
		setOpen(true);
		void refresh();
	}

	function close(): void {
		generation += 1;
		setOpen(false);
		setError("");
	}

	async function selectFile(path: string): Promise<void> {
		if (!open()) return;
		const loadGeneration = ++generation;
		setLoading(true);
		setError("");
		try {
			await loadFile(path, loadGeneration);
		} catch (cause) {
			if (loadGeneration !== generation) return;
			setError(cause instanceof Error ? cause.message : String(cause));
		} finally {
			if (loadGeneration === generation) setLoading(false);
		}
	}

	createEffect(() => {
		const sessionId = snapshot().protocol.serverState.sessionId;
		if (observedSessionId !== undefined && observedSessionId !== sessionId) {
			close();
			setState(null);
			setSelectedFile(null);
		}
		observedSessionId = sessionId;
	});

	const unsubscribeReview = controller.subscribeReview(() => {
		if (open() && snapshot().protocol.phase === "live") void refresh();
	});
	onCleanup(unsubscribeReview);

	return (
		<CodeReviewContext.Provider
			value={{
				open,
				loading,
				error,
				state,
				selectedFile,
				openReview,
				close,
				selectFile,
				refresh,
			}}
		>
			{props.children}
		</CodeReviewContext.Provider>
	);
}
