import { createStore } from "solid-js/store";

export type CodeReviewHostStatus = {
	serverState: "idle" | "starting" | "ready" | "error";
	port: number | null;
	clientConnected: boolean;
	launchInFlight: boolean;
	lastError: string | null;
};

const INITIAL_STATUS: CodeReviewHostStatus = {
	serverState: "idle",
	port: null,
	clientConnected: false,
	launchInFlight: false,
	lastError: null,
};

const [codeReviewStatus, setCodeReviewStatus] =
	createStore<CodeReviewHostStatus>(INITIAL_STATUS);

export { codeReviewStatus };

export function updateCodeReviewStatus(next: CodeReviewHostStatus): void {
	setCodeReviewStatus(next);
}

export function resetCodeReviewStatus(): void {
	setCodeReviewStatus(INITIAL_STATUS);
}
