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
	type RemoteCommandList,
	type RemoteModel,
	type RemoteReviewFile,
	type RemoteReviewNote,
	type RemoteReviewState,
	type RemoteScratchpad,
	WebRemoteServices,
} from "./remote-services";
import { WebSocketRpcTransport } from "./rpc-transport";
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
		this.transport.dispose();
		this.view.dispose();
	}

	isStreaming(): boolean {
		return this.state.serverState.isStreaming === true;
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
		if (this.view.submitting()) return false;
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
			this.view.submitting() ? "Still sending…" : "Kit is not connected",
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
