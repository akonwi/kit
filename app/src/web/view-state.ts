import type { ClientState } from "./client-state";

export type PendingAttachment = {
	file: File;
	id?: string;
	uploadStreamId?: string;
};

export type ClientStatus = {
	message: string;
	isError: boolean;
};

export type WebClientSnapshot = {
	protocol: ClientState;
	status: ClientStatus;
	submitting: boolean;
	loadingEarlier: boolean;
	answeringInteractionId: string | null;
	attachments: PendingAttachment[];
	interactionHydrationErrors: ReadonlyMap<string, string>;
	interactionResponseErrors: ReadonlyMap<string, string>;
};

export class WebClientViewState {
	private submittingValue = false;
	private loadingEarlierValue = false;
	private answeringInteractionIdValue: string | null = null;
	private attachmentsValue: PendingAttachment[] = [];
	private statusValue: ClientStatus = { message: "", isError: false };
	private readonly interactionHydrationErrorsValue = new Map<string, string>();
	private readonly interactionResponseErrorsValue = new Map<string, string>();
	private readonly listeners = new Set<(snapshot: WebClientSnapshot) => void>();

	constructor(private protocolValue: ClientState) {}

	setProtocol(state: ClientState): void {
		this.protocolValue = state;
	}

	snapshot(): WebClientSnapshot {
		return {
			protocol: this.protocolValue,
			status: this.protocolValue.lastError
				? { message: this.protocolValue.lastError, isError: true }
				: this.statusValue,
			submitting: this.submittingValue,
			loadingEarlier: this.loadingEarlierValue,
			answeringInteractionId: this.answeringInteractionIdValue,
			attachments: [...this.attachmentsValue],
			interactionHydrationErrors: new Map(this.interactionHydrationErrorsValue),
			interactionResponseErrors: new Map(this.interactionResponseErrorsValue),
		};
	}

	subscribe(listener: (snapshot: WebClientSnapshot) => void): () => void {
		this.listeners.add(listener);
		listener(this.snapshot());
		return () => this.listeners.delete(listener);
	}

	dispose(): void {
		this.listeners.clear();
	}

	notify(): void {
		const snapshot = this.snapshot();
		for (const listener of this.listeners) listener(snapshot);
	}

	setStatus(message: string, isError = false): void {
		this.statusValue = { message, isError };
	}

	reportError(error: unknown): void {
		this.setStatus(
			error instanceof Error ? error.message : String(error),
			true,
		);
		this.notify();
	}

	submitting(): boolean {
		return this.submittingValue;
	}

	setSubmitting(value: boolean): void {
		this.submittingValue = value;
	}

	loadingEarlier(): boolean {
		return this.loadingEarlierValue;
	}

	setLoadingEarlier(value: boolean): void {
		this.loadingEarlierValue = value;
	}

	attachments(): PendingAttachment[] {
		return [...this.attachmentsValue];
	}

	setAttachments(attachments: PendingAttachment[]): void {
		this.attachmentsValue = attachments;
	}

	answeringInteractionId(): string | null {
		return this.answeringInteractionIdValue;
	}

	setAnsweringInteractionId(requestId: string | null): void {
		this.answeringInteractionIdValue = requestId;
	}

	clearInteractionHydrationError(requestId: string): void {
		this.interactionHydrationErrorsValue.delete(requestId);
	}

	setInteractionHydrationError(requestId: string, message: string): void {
		this.interactionHydrationErrorsValue.set(requestId, message);
	}

	clearInteractionResponseError(requestId: string): void {
		this.interactionResponseErrorsValue.delete(requestId);
	}

	setInteractionResponseError(requestId: string, message: string): void {
		this.interactionResponseErrorsValue.set(requestId, message);
	}

	reconcilePendingInteractionIds(pendingIds: ReadonlySet<string>): void {
		for (const requestId of this.interactionResponseErrorsValue.keys()) {
			if (!pendingIds.has(requestId)) {
				this.interactionResponseErrorsValue.delete(requestId);
			}
		}
		if (
			this.answeringInteractionIdValue &&
			!pendingIds.has(this.answeringInteractionIdValue)
		) {
			this.answeringInteractionIdValue = null;
		}
	}
}
