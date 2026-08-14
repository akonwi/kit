import {
	type ClientState,
	createClientState,
	hydrateMessageReference,
	isRecord,
	ProtocolRebaseRequired,
	prependMessages,
	reduceClientRecord,
	withConnectionPhase,
} from "./client-state";
import {
	applyRestoredComposerDraft,
	readComposerDraft,
} from "./composer-draft-storage";
import {
	type RemoteCommandList,
	type RemoteModel,
	type RemoteReviewFile,
	type RemoteReviewNote,
	type RemoteReviewState,
	type RemoteScratchpad,
	WebRemoteServices,
} from "./remote-services";
import {
	RpcCommandError,
	RpcResponseLostError,
	WebSocketRpcTransport,
} from "./rpc-transport";
import {
	type PendingAttachment,
	type WebClientSnapshot,
	WebClientViewState,
} from "./view-state";
import { toastForProtocolRecord, type WebToastSink } from "./web-toasts";

export type {
	ClientStatus,
	PendingAttachment,
	WebClientSnapshot,
} from "./view-state";

export type WebClientControllerOptions = {
	showToast?: WebToastSink;
};

export type PendingPromotion = {
	streamId: string | null;
	sessionId: string;
	generation: number;
};

export type PendingRestoreClaim = {
	operationId: string;
	streamId: string;
	sessionId: string;
	generation: number;
	status: "claiming" | "restored" | "applied";
	messages?: string[];
	applyDraft?: (messages: string[], operationId: string) => boolean;
};

const FOLLOW_UP_CLIENT_ID_KEY = "kit.follow-ups.client-id";
const FOLLOW_UP_TAB_ID_KEY = "kit.follow-ups.tab-id";
const PENDING_RESTORE_KEY_PREFIX = "kit.follow-ups.pending-restore.";
const PENDING_RESTORE_POINTER_KEY = "kit.follow-ups.pending-restore-id";
type RestoreStorage = Pick<
	Storage,
	"getItem" | "setItem" | "removeItem" | "key" | "length"
>;

function storedBrowserId(storage: Storage, key: string): string {
	try {
		const stored = storage.getItem(key);
		if (stored && /^[A-Za-z0-9._:-]{1,128}$/.test(stored)) return stored;
	} catch {
		// Browser storage is optional for identifiers.
	}
	const created = crypto.randomUUID();
	try {
		storage.setItem(key, created);
	} catch {
		// Keep the in-memory id when browser storage is unavailable.
	}
	return created;
}

function followUpClientId(): string {
	return storedBrowserId(localStorage, FOLLOW_UP_CLIENT_ID_KEY);
}

function followUpTabId(): string {
	return storedBrowserId(sessionStorage, FOLLOW_UP_TAB_ID_KEY);
}

function pendingRestoreKey(clientId: string, operationId: string): string {
	return `${PENDING_RESTORE_KEY_PREFIX}${encodeURIComponent(clientId)}.${encodeURIComponent(operationId)}`;
}

function pendingRestorePointer(): string | null {
	try {
		return sessionStorage.getItem(PENDING_RESTORE_POINTER_KEY);
	} catch {
		return null;
	}
}

function setPendingRestorePointer(operationId: string | null): void {
	try {
		if (operationId) {
			sessionStorage.setItem(PENDING_RESTORE_POINTER_KEY, operationId);
		} else {
			sessionStorage.removeItem(PENDING_RESTORE_POINTER_KEY);
		}
	} catch {
		// The durable operation record remains discoverable without a pointer.
	}
}

export function readPendingRestore(
	clientId: string,
	storage: RestoreStorage = localStorage,
	operationId?: string | null,
): PendingRestoreClaim | null {
	try {
		let value = operationId
			? storage.getItem(pendingRestoreKey(clientId, operationId))
			: null;
		if (!value) {
			const prefix = `${PENDING_RESTORE_KEY_PREFIX}${encodeURIComponent(clientId)}.`;
			for (let index = 0; index < storage.length; index += 1) {
				const key = storage.key(index);
				if (!key?.startsWith(prefix)) continue;
				value = storage.getItem(key);
				if (value) break;
			}
		}
		if (!value) return null;
		const parsed: unknown = JSON.parse(value);
		if (
			!isRecord(parsed) ||
			parsed.clientId !== clientId ||
			typeof parsed.operationId !== "string" ||
			typeof parsed.streamId !== "string" ||
			typeof parsed.sessionId !== "string" ||
			typeof parsed.generation !== "number" ||
			!Number.isSafeInteger(parsed.generation) ||
			parsed.generation < 0 ||
			(parsed.status !== "claiming" &&
				parsed.status !== "restored" &&
				parsed.status !== "applied") ||
			(parsed.status === "restored" &&
				(!Array.isArray(parsed.messages) ||
					!parsed.messages.every((message) => typeof message === "string")))
		) {
			return null;
		}
		return {
			operationId: parsed.operationId,
			streamId: parsed.streamId,
			sessionId: parsed.sessionId,
			generation: parsed.generation,
			status: parsed.status,
			...(parsed.status === "restored"
				? { messages: parsed.messages as string[] }
				: {}),
		};
	} catch {
		return null;
	}
}

export function writePendingRestore(
	clientId: string,
	claim: PendingRestoreClaim,
	storage: RestoreStorage = localStorage,
): boolean {
	try {
		storage.setItem(
			pendingRestoreKey(clientId, claim.operationId),
			JSON.stringify({
				clientId,
				operationId: claim.operationId,
				streamId: claim.streamId,
				sessionId: claim.sessionId,
				generation: claim.generation,
				status: claim.status,
				...(claim.status === "restored" ? { messages: claim.messages } : {}),
			}),
		);
		return true;
	} catch {
		return false;
	}
}

export function clearPendingRestore(
	clientId: string,
	operationId: string,
	storage: RestoreStorage = localStorage,
): void {
	try {
		storage.removeItem(pendingRestoreKey(clientId, operationId));
	} catch {
		// The retained server claim remains safe when storage cleanup fails.
	}
}

function persistentToastKey(record: unknown): string | null {
	return isRecord(record) &&
		record.type === "ui.toast.requested" &&
		isRecord(record.toast) &&
		record.toast.persistent === true
		? JSON.stringify(record.toast)
		: null;
}

export class WebClientController {
	private state: ClientState;
	private readonly view: WebClientViewState;
	private readonly transport: WebSocketRpcTransport;
	private readonly services: WebRemoteServices;
	private activeSyncMode: string | null = null;
	private loadingPendingInteractions = false;
	private capabilitiesStreamId: string | null = null;
	private loadingCapabilities = false;
	private readonly hydratingMessages = new Set<string>();
	private readonly hydratingInteractions = new Set<string>();
	private readonly showToast?: WebToastSink;
	private disposed = false;
	private readonly seenSnapshotToasts = new Set<string>();
	private readonly reviewListeners = new Set<() => void>();
	private readonly followUpClientId = followUpClientId();
	private readonly followUpTabId = followUpTabId();
	private pendingRestore: PendingRestoreClaim | null = null;
	private pendingPromotion: PendingPromotion | null = null;
	private restoringFollowUps = false;
	private acknowledgingRestores = false;
	private lastProtocolToast: {
		sequence: number;
		type: string;
		description?: string;
	} | null = null;

	constructor(options: WebClientControllerOptions = {}) {
		this.state = createClientState();
		this.showToast = options.showToast;
		this.view = new WebClientViewState(this.state, options.showToast);
		this.transport = new WebSocketRpcTransport({
			getResumeCursor: () =>
				this.state.streamId
					? { streamId: this.state.streamId, sequence: this.state.sequence }
					: null,
			onConnecting: () => {
				this.setState(withConnectionPhase(this.state, "connecting"));
				this.view.notify();
			},
			onDisconnected: () => {
				this.setState(withConnectionPhase(this.state, "disconnected"));
				this.view.notify();
			},
			onProtocolRecords: (records) => this.reduceProtocolRecords(records),
			onError: (error) => {
				if (error instanceof ProtocolRebaseRequired) return;
				this.view.reportError(error, "Connection error");
			},
		});
		this.services = new WebRemoteServices({
			command: (command) => this.sendCommand(command),
		});
		this.pendingRestore = readPendingRestore(
			this.followUpClientId,
			localStorage,
			pendingRestorePointer(),
		);
		if (this.pendingRestore) {
			setPendingRestorePointer(this.pendingRestore.operationId);
			this.view.setFollowUpMutationPending(true);
			this.view.setStatus(
				this.pendingRestore.status === "applied"
					? "Finalizing restored follow-ups…"
					: "Recovering queued follow-ups…",
			);
		}
	}

	snapshot(): WebClientSnapshot {
		return this.view.snapshot();
	}

	subscribe(listener: (snapshot: WebClientSnapshot) => void): () => void {
		return this.view.subscribe(listener);
	}

	subscribeReview(listener: () => void): () => void {
		this.reviewListeners.add(listener);
		return () => this.reviewListeners.delete(listener);
	}

	start(): void {
		this.transport.start();
	}

	reconnect(): void {
		this.transport.reconnectNow();
	}

	dispose(): void {
		this.disposed = true;
		this.pendingRestore = null;
		this.transport.dispose();
		this.view.dispose();
	}

	isStreaming(): boolean {
		return this.state.serverState.isStreaming === true;
	}

	composerDraftScopeId(): string {
		return this.followUpTabId;
	}

	addAttachments(files: File[]): void {
		if (this.view.submitting() || files.length === 0) return;
		const attachments = this.view.attachments();
		const error = this.services.validateAttachments(attachments, files);
		if (error) {
			this.view.setStatus(error, true);
			this.view.notify();
			return;
		}
		this.view.setAttachments([
			...attachments,
			...files.map((file) => ({ file })),
		]);
		this.view.setStatus(
			`${this.view.attachments().length} attachments selected`,
		);
		this.view.notify();
	}

	async removeAttachment(attachment: PendingAttachment): Promise<void> {
		const attachments = this.view
			.attachments()
			.filter((item) => item !== attachment);
		this.view.setAttachments(attachments);
		this.view.setStatus(
			attachments.length === 0
				? "Attachment removed"
				: `${attachments.length} attachments selected`,
		);
		this.view.notify();
		if (!attachment.id || attachment.uploadStreamId !== this.state.streamId) {
			return;
		}
		try {
			await this.services.removeAttachment(attachment.id);
		} catch (error) {
			this.view.reportError(error, "Attachment removal failed");
		}
	}

	async submit(messageValue: string): Promise<boolean> {
		if (this.view.submitting() || this.view.followUpMutationPending()) {
			return false;
		}
		if (!this.transport.drainProtocolRecords()) {
			this.view.reportError(new Error("Protocol synchronization failed"));
			return false;
		}
		const message = messageValue.trim();
		const submittedAttachments = this.view.attachments();
		const submissionWasStreaming = this.isStreaming();
		if (!message && submittedAttachments.length === 0) return false;
		if (submissionWasStreaming && submittedAttachments.length > 0) {
			this.view.setStatus(
				"Remove attachments before sending a follow-up message",
				true,
			);
			this.view.notify();
			return false;
		}
		const submissionStreamId = this.state.streamId;
		const submissionSessionId = this.state.serverState.sessionId;
		this.view.setSubmitting(true);
		this.view.setStatus(
			submittedAttachments.length > 0 ? "Uploading attachments…" : "Sending…",
		);
		this.view.notify();
		try {
			const attachmentIds: string[] = [];
			for (let index = 0; index < submittedAttachments.length; index += 1) {
				const attachment = submittedAttachments[index];
				if (!attachment) continue;
				let id =
					attachment.uploadStreamId === submissionStreamId
						? attachment.id
						: undefined;
				if (!id) {
					id = await this.services.uploadAttachment(attachment.file);
					const uploaded = {
						...attachment,
						id,
						uploadStreamId: submissionStreamId ?? undefined,
					};
					submittedAttachments[index] = uploaded;
					this.view.setAttachments(
						this.view
							.attachments()
							.map((item) => (item === attachment ? uploaded : item)),
					);
				}
				attachmentIds.push(id);
			}
			await this.sendCommand(
				{
					type: "prompt",
					message,
					...(attachmentIds.length > 0 ? { attachmentIds } : {}),
					...(submissionWasStreaming ? { streamingBehavior: "followUp" } : {}),
				},
				() => {
					if (
						this.state.streamId !== submissionStreamId ||
						this.state.serverState.sessionId !== submissionSessionId
					) {
						throw new Error(
							"The Kit session changed before the prompt was sent",
						);
					}
				},
			);
			this.view.setAttachments(
				this.view
					.attachments()
					.filter((attachment) => !submittedAttachments.includes(attachment)),
			);
			this.view.setStatus("");
			return true;
		} catch (error) {
			this.view.reportError(error, "Message failed");
			return false;
		} finally {
			this.view.setSubmitting(false);
			this.view.notify();
		}
	}

	reportComposerUnavailable(): void {
		this.view.setStatus(
			this.view.submitting()
				? "Still sending…"
				: this.view.followUpMutationPending()
					? "Finishing queued follow-up restore…"
					: "Kit is not connected",
			true,
		);
		this.view.notify();
	}

	async abort(): Promise<void> {
		try {
			await this.sendCommand({ type: "abort" });
		} catch (error) {
			this.view.reportError(error, "Abort failed");
		}
	}

	async restoreQueuedFollowUps(
		applyDraft: (messages: string[], operationId: string) => boolean,
	): Promise<boolean> {
		if (this.view.followUpMutationPending() || this.pendingRestore)
			return false;
		const sessionId = this.state.serverState.sessionId;
		const streamId = this.state.streamId;
		if (typeof sessionId !== "string" || !streamId) {
			this.view.reportError(
				new Error("Active session is unavailable"),
				"Restore failed",
			);
			return false;
		}
		const claim: PendingRestoreClaim = {
			operationId: crypto.randomUUID(),
			streamId,
			sessionId,
			generation: this.state.queuedMessageGeneration,
			status: "claiming",
			applyDraft,
		};
		if (!writePendingRestore(this.followUpClientId, claim)) {
			this.view.reportError(
				new Error("Browser storage is unavailable"),
				"Restore failed",
			);
			return false;
		}
		setPendingRestorePointer(claim.operationId);
		this.pendingRestore = claim;
		this.view.setFollowUpMutationPending(true);
		this.view.setStatus("Restoring queued follow-ups…");
		this.view.notify();
		return this.continuePendingRestore();
	}

	resumeQueuedFollowUpRestore(
		applyDraft: (messages: string[], operationId: string) => boolean,
	): void {
		if (!this.pendingRestore) return;
		this.pendingRestore.applyDraft = applyDraft;
		if (this.state.phase === "live") void this.continuePendingRestore();
	}

	async promoteQueuedFollowUps(): Promise<boolean> {
		if (this.view.followUpMutationPending()) return false;
		const sessionId = this.state.serverState.sessionId;
		if (typeof sessionId !== "string") {
			this.view.reportError(
				new Error("Active session is unavailable"),
				"Send-now failed",
			);
			return false;
		}
		const promotion: PendingPromotion = {
			streamId: this.state.streamId,
			sessionId,
			generation: this.state.queuedMessageGeneration,
		};
		this.view.setFollowUpMutationPending(true);
		this.view.setStatus("Sending queued follow-ups now…");
		this.view.notify();
		try {
			await this.services.promoteFollowUps(sessionId, promotion.generation);
			this.view.setStatus("");
			return true;
		} catch (error) {
			if (error instanceof RpcResponseLostError) {
				this.pendingPromotion = promotion;
				this.view.setStatus("Reconnecting to verify queued follow-ups…");
				return false;
			}
			this.view.reportError(error, "Send-now failed");
			return false;
		} finally {
			if (!this.pendingPromotion) {
				this.view.setFollowUpMutationPending(false);
			}
			this.view.notify();
		}
	}

	listModels(): Promise<RemoteModel[]> {
		return this.services.listModels();
	}

	async setModel(model: RemoteModel): Promise<boolean> {
		try {
			await this.services.setModel(model);
			this.view.setStatus("");
			this.view.notify();
			return true;
		} catch (error) {
			this.view.reportError(error, "Model change failed");
			return false;
		}
	}

	listThinkingLevels(): Promise<string[]> {
		return this.services.listThinkingLevels();
	}

	async setThinkingLevel(level: string): Promise<boolean> {
		try {
			await this.services.setThinkingLevel(level);
			this.view.setStatus("");
			this.view.notify();
			return true;
		} catch (error) {
			this.view.reportError(error, "Thinking-level change failed");
			return false;
		}
	}

	listCommands(): Promise<RemoteCommandList> {
		return this.services.listCommands();
	}

	getScratchpad(): Promise<RemoteScratchpad> {
		return this.services.getScratchpad();
	}

	updateScratchpad(
		sessionId: string,
		expectedContent: string,
		content: string,
	): Promise<RemoteScratchpad> {
		return this.services.updateScratchpad(sessionId, expectedContent, content);
	}

	getReviewState(): Promise<RemoteReviewState> {
		return this.services.getReviewState();
	}

	getReviewFile(path: string): Promise<RemoteReviewFile> {
		return this.services.getReviewFile(path);
	}

	submitReview(
		submissionId: string,
		sessionId: string,
		generation: number,
		notes: RemoteReviewNote[],
	): Promise<Record<string, unknown>> {
		const expectedStreamId = this.state.streamId;
		return this.sendCommand(
			{ type: "submit_review", submissionId, sessionId, generation, notes },
			() => {
				if (
					this.state.streamId !== expectedStreamId ||
					this.state.serverState.sessionId !== sessionId
				) {
					throw new Error("The Kit session changed before the review was sent");
				}
				if (this.state.serverState.isStreaming === true) {
					throw new Error("Wait for the current response before submitting");
				}
			},
		);
	}

	async renameSession(name: string): Promise<boolean> {
		const expectedStreamId = this.state.streamId;
		const expectedSessionId = this.state.serverState.sessionId;
		try {
			if (typeof expectedSessionId !== "string") {
				throw new Error("Active session is unavailable");
			}
			const commands = await this.services.listCommands();
			if (!commands.commands.some((command) => command.id === "name")) {
				throw new Error("Session naming command is unavailable");
			}
			if (
				this.state.phase !== "live" ||
				this.state.streamId !== expectedStreamId ||
				this.state.serverState.sessionId !== expectedSessionId ||
				this.state.serverState.isStreaming === true ||
				this.view.submitting()
			) {
				throw new Error("Session changed before it could be renamed");
			}
			await this.services.executeCommand(
				"name",
				name,
				commands.registryGeneration,
				expectedSessionId,
			);
			this.view.setStatus("");
			this.view.notify();
			return true;
		} catch (error) {
			this.view.reportError(error, "Rename failed");
			return false;
		}
	}

	async activateChromeContribution(
		area: "header" | "footer",
		contributionId: string,
	): Promise<void> {
		try {
			await this.services.activateChromeContribution(area, contributionId);
		} catch (error) {
			this.view.reportError(error, "Plugin action failed");
		}
	}

	async executeCommand(
		commandId: string,
		args: string,
		registryGeneration: number,
	): Promise<boolean> {
		const startingSequence = this.state.sequence;
		try {
			await this.services.executeCommand(commandId, args, registryGeneration);
			this.view.setStatus("");
			this.view.notify();
			return true;
		} catch (error) {
			const errorMessage =
				error instanceof Error ? error.message : String(error);
			const failureAlreadyPresented =
				commandId === "compact" &&
				this.lastProtocolToast?.type === "session.compaction.failed.manual" &&
				this.lastProtocolToast.sequence > startingSequence &&
				this.lastProtocolToast.description === errorMessage;
			if (!failureAlreadyPresented) {
				this.view.reportError(error, "Command failed");
			}
			return false;
		}
	}

	async answerInteraction(
		requestId: string,
		response: unknown,
	): Promise<boolean> {
		if (this.view.answeringInteractionId()) return false;
		this.view.clearInteractionResponseError(requestId);
		this.view.setAnsweringInteractionId(requestId);
		this.view.notify();
		try {
			await this.sendCommand({ type: "ui_response", requestId, response });
			return true;
		} catch (error) {
			this.view.setAnsweringInteractionId(null);
			this.view.setInteractionResponseError(
				requestId,
				error instanceof Error ? error.message : String(error),
			);
			this.view.reportError(error);
			this.view.notify();
			return false;
		}
	}

	reportInteractionError(requestId: string, message: string): void {
		this.view.setInteractionResponseError(requestId, message);
		this.view.notify();
	}

	ensureInteractionHydrated(requestId: string): void {
		void this.hydrateInteraction(requestId);
	}

	retryInteraction(requestId: string): void {
		this.view.clearInteractionHydrationError(requestId);
		this.view.notify();
		this.ensureInteractionHydrated(requestId);
	}

	async loadEarlier(beforeCommit?: () => void): Promise<void> {
		if (this.view.loadingEarlier() || this.state.messageOffset === 0) return;
		this.view.setLoadingEarlier(true);
		this.view.notify();
		const streamId = this.state.streamId;
		const oldOffset = this.state.messageOffset;
		try {
			const targetOffset = Math.max(0, oldOffset - 50);
			const range = await this.services.loadMessageRange(
				targetOffset,
				oldOffset,
				this.state.totalMessageCount,
			);
			if (
				this.state.phase !== "live" ||
				this.state.streamId !== streamId ||
				this.state.messageOffset !== oldOffset
			) {
				return;
			}
			beforeCommit?.();
			this.setState(
				prependMessages(
					this.state,
					range.messages,
					targetOffset,
					range.totalMessageCount,
				),
			);
			this.view.setStatus("");
		} catch (error) {
			this.view.reportError(error);
		} finally {
			this.view.setLoadingEarlier(false);
			this.view.notify();
		}
	}

	private reconcilePendingPromotion(): void {
		const promotion = this.pendingPromotion;
		if (!promotion || this.state.phase !== "live") return;
		const unchanged =
			this.state.streamId === promotion.streamId &&
			this.state.serverState.sessionId === promotion.sessionId &&
			this.state.queuedMessageGeneration === promotion.generation &&
			this.state.queuedMessageCount > 0;
		this.pendingPromotion = null;
		this.view.setFollowUpMutationPending(false);
		this.view.setStatus(
			unchanged
				? "Send now was not applied; the queued follow-ups are still available"
				: "Queued follow-up state reconciled after reconnecting",
			unchanged,
		);
		this.view.notify();
	}

	private recoverNextPendingRestore(
		applyDraft?: (messages: string[], operationId: string) => boolean,
	): void {
		const next = readPendingRestore(this.followUpClientId);
		if (!next) {
			this.pendingRestore = null;
			setPendingRestorePointer(null);
			this.view.setFollowUpMutationPending(false);
			return;
		}
		next.applyDraft = applyDraft;
		this.pendingRestore = next;
		setPendingRestorePointer(next.operationId);
		this.view.setFollowUpMutationPending(true);
		this.view.setStatus(
			next.status === "applied"
				? "Finalizing restored follow-ups…"
				: "Recovering queued follow-ups…",
		);
		if (this.state.phase === "live") void this.continuePendingRestore();
	}

	private async continuePendingRestore(): Promise<boolean> {
		let claim = this.pendingRestore;
		if (!claim || this.restoringFollowUps || this.disposed) return false;
		if (claim.status === "applied") {
			void this.finalizePendingRestore();
			return true;
		}
		if (claim.status === "claiming" && this.state.phase !== "live") {
			this.view.setStatus("Reconnecting to recover queued follow-ups…");
			this.view.notify();
			return false;
		}
		if (claim.status === "claiming" && this.state.streamId !== claim.streamId) {
			clearPendingRestore(this.followUpClientId, claim.operationId);
			this.recoverNextPendingRestore(claim.applyDraft);
			this.view.reportError(
				new Error("The Kit host restarted before the restore was confirmed"),
				"Restore could not be recovered",
			);
			return false;
		}
		this.restoringFollowUps = true;
		try {
			if (claim.status === "claiming") {
				const restored = await this.services.restoreFollowUps(
					this.followUpClientId,
					claim.operationId,
					claim.sessionId,
					claim.generation,
				);
				if (this.pendingRestore !== claim) return false;
				claim = {
					...claim,
					status: "restored",
					messages: restored.messages,
				};
				if (!writePendingRestore(this.followUpClientId, claim)) {
					this.pendingRestore = claim;
					this.view.setStatus(
						"Restored follow-ups are safe on the server, but browser storage is unavailable",
						true,
					);
					this.view.notify();
					return false;
				}
				this.pendingRestore = claim;
			}
			const messages = claim.messages;
			if (claim.status !== "restored" || !messages) return false;
			const restoredDraft = applyRestoredComposerDraft(
				this.followUpTabId,
				claim.sessionId,
				claim.operationId,
				messages,
				readComposerDraft(this.followUpTabId, claim.sessionId),
			);
			if (restoredDraft === null) {
				this.view.setStatus(
					"Restored follow-ups are safe, but browser storage is unavailable",
					true,
				);
				this.view.notify();
				return false;
			}
			if (this.state.serverState.sessionId === claim.sessionId) {
				const applyDraft = claim.applyDraft;
				if (!applyDraft || !applyDraft(messages, claim.operationId)) {
					this.view.setStatus(
						"Restored follow-ups are safe, but the composer is unavailable",
						true,
					);
					this.view.notify();
					return false;
				}
			}
			claim = { ...claim, status: "applied", messages: undefined };
			if (!writePendingRestore(this.followUpClientId, claim)) {
				this.pendingRestore = claim;
				this.view.setStatus(
					"Follow-ups are restored, but recovery state could not be saved",
					true,
				);
				this.view.notify();
				return false;
			}
			this.pendingRestore = claim;
			this.view.setStatus("Finalizing restored follow-ups…");
			this.view.notify();
			void this.finalizePendingRestore();
			return true;
		} catch (error) {
			if (this.disposed || this.pendingRestore !== claim) return false;
			if (error instanceof RpcCommandError) {
				clearPendingRestore(this.followUpClientId, claim.operationId);
				this.recoverNextPendingRestore(claim.applyDraft);
				this.view.reportError(error, "Restore failed");
				return false;
			}
			this.view.setStatus(
				error instanceof RpcResponseLostError
					? "Reconnecting to recover queued follow-ups…"
					: "Waiting to retry queued follow-up recovery…",
			);
			this.view.notify();
			return false;
		} finally {
			this.restoringFollowUps = false;
		}
	}

	private async finalizePendingRestore(): Promise<void> {
		const claim = this.pendingRestore;
		if (
			!claim ||
			claim.status !== "applied" ||
			this.acknowledgingRestores ||
			this.state.phase !== "live" ||
			this.disposed
		) {
			return;
		}
		this.acknowledgingRestores = true;
		try {
			await this.services.acknowledgeFollowUpMutation(
				this.followUpClientId,
				claim.operationId,
			);
			if (this.pendingRestore !== claim) return;
			clearPendingRestore(this.followUpClientId, claim.operationId);
			this.recoverNextPendingRestore(claim.applyDraft);
			if (!this.pendingRestore) this.view.setStatus("");
			this.view.notify();
		} catch {
			this.view.setStatus("Waiting to finalize restored follow-ups…");
			this.view.notify();
		} finally {
			this.acknowledgingRestores = false;
		}
	}

	private setState(state: ClientState): void {
		this.state = state;
		this.view.setProtocol(state);
	}

	private messageDelta(record: unknown): Record<string, unknown> | null {
		if (
			!isRecord(record) ||
			record.type !== "agent.message.updated" ||
			!isRecord(record.update)
		) {
			return null;
		}
		const update = record.update;
		return update.kind === "content.delta" &&
			typeof update.delta === "string" &&
			typeof update.contentIndex === "number" &&
			typeof update.contentType === "string"
			? update
			: null;
	}

	private reduceProtocolRecords(records: readonly unknown[]): void {
		const previousStreamId = this.state.streamId;
		for (let index = 0; index < records.length; index += 1) {
			let record = records[index];
			if (isRecord(record) && record.type === "review.changed") {
				for (const listener of this.reviewListeners) listener();
			}
			const delta = this.messageDelta(record);
			if (delta && isRecord(record) && typeof record.sequence === "number") {
				let combinedDelta = delta.delta as string;
				let lastSequence = record.sequence;
				while (index + 1 < records.length) {
					const next = records[index + 1];
					const nextDelta = this.messageDelta(next);
					if (
						!isRecord(next) ||
						!nextDelta ||
						next.streamId !== record.streamId ||
						next.messageId !== record.messageId ||
						next.sequence !== lastSequence + 1 ||
						nextDelta.kind !== delta.kind ||
						nextDelta.contentType !== delta.contentType ||
						nextDelta.contentIndex !== delta.contentIndex
					) {
						break;
					}
					combinedDelta += nextDelta.delta as string;
					lastSequence = next.sequence as number;
					index += 1;
				}
				if (lastSequence !== record.sequence) {
					record = {
						...record,
						update: { ...delta, delta: combinedDelta },
					};
					this.setState(reduceClientRecord(this.state, record));
					this.setState({ ...this.state, sequence: lastSequence });
					continue;
				}
			}
			if (isRecord(record) && record.type === "sync") {
				this.activeSyncMode =
					typeof record.mode === "string" ? record.mode : null;
				if (record.mode === "snapshot" && Array.isArray(record.toasts)) {
					for (const toast of record.toasts) {
						const key = JSON.stringify(toast);
						if (this.seenSnapshotToasts.has(key)) continue;
						const toastRecord = { type: "ui.toast.requested", toast };
						const input = toastForProtocolRecord(toastRecord);
						if (input && this.presentProtocolToast(input, toastRecord)) {
							this.seenSnapshotToasts.add(key);
						}
					}
				}
			}
			if (isRecord(record) && record.type === "resync_required") {
				throw new ProtocolRebaseRequired(
					"The session requires a fresh snapshot",
				);
			}
			const toast =
				(this.activeSyncMode === null && this.state.phase === "live") ||
				this.activeSyncMode === "replay"
					? toastForProtocolRecord(record)
					: null;
			const previousSequence = this.state.sequence;
			this.setState(reduceClientRecord(this.state, record));
			if (toast && this.state.sequence > previousSequence) {
				this.presentProtocolToast(toast, record);
			}
			if (isRecord(record) && record.type === "sync_complete") {
				if (this.activeSyncMode === "snapshot") {
					this.transport.acceptSnapshot();
				}
				this.activeSyncMode = null;
			}
		}
		if (this.state.streamId !== previousStreamId) {
			this.view.setAttachments(
				this.view.attachments().map(({ file }) => ({ file })),
			);
			this.services.resetLimits();
			this.capabilitiesStreamId = null;
		}
		this.reconcileInteractions();
		if (this.state.phase === "live") {
			this.transport.resetReconnectBackoff();
			this.reconcilePendingPromotion();
			if (this.pendingRestore && !this.restoringFollowUps) {
				void this.continuePendingRestore();
			}
			void this.loadCapabilities();
			void this.loadPendingInteractions();
			void this.hydrateVisibleMessageReferences();
		}
		this.view.notify();
	}

	private presentProtocolToast(
		input: Parameters<WebToastSink>[0],
		record: unknown,
	): boolean {
		if (this.disposed || !this.showToast) return false;
		try {
			this.showToast(input);
			this.lastProtocolToast = {
				sequence: this.state.sequence,
				type:
					isRecord(record) && typeof record.type === "string"
						? record.type
						: "",
				description: input.description,
			};
			const persistentKey = persistentToastKey(record);
			if (persistentKey) this.seenSnapshotToasts.add(persistentKey);
			return true;
		} catch {
			this.view.setStatus(
				input.description ?? input.title,
				input.variant === "error",
			);
			return false;
		}
	}

	private reconcileInteractions(): void {
		const pendingIds = new Set<string>();
		for (const request of this.state.pendingInteractions) {
			if (isRecord(request) && typeof request.id === "string") {
				pendingIds.add(request.id);
			}
		}
		this.view.reconcilePendingInteractionIds(pendingIds);
	}

	private sendCommand(
		command: Record<string, unknown>,
		beforeSend?: () => void,
	): Promise<Record<string, unknown>> {
		if (!this.transport.drainProtocolRecords()) {
			return Promise.reject(new Error("Protocol synchronization failed"));
		}
		try {
			beforeSend?.();
		} catch (error) {
			return Promise.reject(
				error instanceof Error ? error : new Error(String(error)),
			);
		}
		if (this.state.phase !== "live" || !this.transport.isOpen()) {
			return Promise.reject(new Error("Kit is not connected"));
		}
		return this.transport.command(command);
	}

	private async loadCapabilities(): Promise<void> {
		const streamId = this.state.streamId;
		if (
			!streamId ||
			this.loadingCapabilities ||
			this.capabilitiesStreamId === streamId
		) {
			return;
		}
		this.loadingCapabilities = true;
		try {
			const limits = await this.services.fetchLimits();
			if (this.state.streamId !== streamId) return;
			this.services.installLimits(limits);
			this.capabilitiesStreamId = streamId;
		} catch (error) {
			this.view.reportError(error);
		} finally {
			this.loadingCapabilities = false;
			if (this.state.phase === "live" && this.state.streamId !== streamId) {
				void this.loadCapabilities();
			}
		}
	}

	private async hydrateVisibleMessageReferences(): Promise<void> {
		for (const [index, message] of this.state.messages.entries()) {
			if (
				!isRecord(message) ||
				message.type !== "message_reference" ||
				typeof message.token !== "string" ||
				this.hydratingMessages.has(message.token)
			) {
				continue;
			}
			this.hydratingMessages.add(message.token);
			try {
				const messageIndex =
					typeof message.messageIndex === "number"
						? message.messageIndex
						: this.state.messageOffset + index;
				const recovered = await this.services.recoverMessageReference(
					message,
					messageIndex,
					() => this.state.phase === "live",
				);
				const currentIndex = this.state.messages.indexOf(message);
				if (currentIndex < 0) continue;
				this.setState(
					hydrateMessageReference(
						this.state,
						currentIndex,
						message,
						recovered.message,
						!recovered.rebased,
					),
				);
				this.view.notify();
			} catch (error) {
				this.view.reportError(error);
			} finally {
				this.hydratingMessages.delete(message.token);
			}
		}
	}

	private async loadPendingInteractions(): Promise<void> {
		if (
			this.loadingPendingInteractions ||
			this.state.phase !== "live" ||
			this.state.pendingInteractions.length >=
				this.state.totalPendingInteractionCount
		) {
			return;
		}
		this.loadingPendingInteractions = true;
		const generation = this.state.pendingInteractionGeneration;
		try {
			const result = await this.services.loadPendingInteractions(
				generation,
				this.state.totalPendingInteractionCount,
				() => this.state.pendingInteractionGeneration === generation,
			);
			if (!result || this.state.pendingInteractionGeneration !== generation) {
				return;
			}
			this.setState({
				...this.state,
				pendingInteractions: result.requests,
				pendingInteractionOffset: 0,
				totalPendingInteractionCount: result.totalRequestCount,
			});
			this.view.notify();
		} catch (error) {
			this.view.reportError(error);
		} finally {
			this.loadingPendingInteractions = false;
			if (
				this.state.phase === "live" &&
				this.state.pendingInteractionGeneration !== generation &&
				this.state.pendingInteractions.length <
					this.state.totalPendingInteractionCount
			) {
				void this.loadPendingInteractions();
			}
		}
	}

	private async hydrateInteraction(requestId: string): Promise<void> {
		if (
			this.hydratingInteractions.has(requestId) ||
			this.state.phase !== "live"
		) {
			return;
		}
		this.hydratingInteractions.add(requestId);
		try {
			const hydrated = await this.services.recoverInteraction(requestId);
			if (
				!this.state.pendingInteractions.some(
					(request) => isRecord(request) && request.id === requestId,
				)
			) {
				return;
			}
			this.setState({
				...this.state,
				pendingInteractions: this.state.pendingInteractions.map((request) =>
					isRecord(request) && request.id === requestId ? hydrated : request,
				),
			});
			this.view.clearInteractionHydrationError(requestId);
		} catch (error) {
			if (
				this.state.pendingInteractions.some(
					(request) => isRecord(request) && request.id === requestId,
				)
			) {
				const message = error instanceof Error ? error.message : String(error);
				this.view.setInteractionHydrationError(requestId, message);
				this.view.setStatus(message, true);
			}
		} finally {
			this.hydratingInteractions.delete(requestId);
			this.view.notify();
		}
	}
}
