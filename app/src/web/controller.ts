import {
	type ClientState,
	createClientState,
	hydrateMessageReference,
	isRecord,
	ProtocolSyncError,
	prependMessages,
	reduceClientRecord,
	withConnectionPhase,
} from "./client-state";

export type PendingAttachment = {
	file: File;
	id?: string;
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

type PendingCommand = {
	resolve(record: Record<string, unknown>): void;
	reject(error: Error): void;
};

type RecoveredMessage = {
	message: unknown;
	rebased: boolean;
};

const MAX_INTERACTION_BYTES = 2 * 1024 * 1024;

export class WebClientController {
	private state = createClientState();
	private socket: WebSocket | null = null;
	private reconnectTimer: number | null = null;
	private reconnectAttempt = 0;
	private requireSnapshot = false;
	private activeSyncMode: string | null = null;
	private submitting = false;
	private loadingPendingInteractions = false;
	private commandCounter = 0;
	private pendingAttachments: PendingAttachment[] = [];
	private loadingEarlier = false;
	private answeringInteractionId: string | null = null;
	private status: ClientStatus = { message: "", isError: false };
	private readonly interactionHydrationErrors = new Map<string, string>();
	private readonly interactionResponseErrors = new Map<string, string>();
	private readonly hydratingMessages = new Set<string>();
	private readonly hydratingInteractions = new Set<string>();
	private readonly pendingCommands = new Map<string, PendingCommand>();
	private readonly queuedRecords: unknown[] = [];
	private protocolTimer: number | null = null;
	private readonly listeners = new Set<(snapshot: WebClientSnapshot) => void>();
	private started = false;

	private readonly onlineListener = () => {
		if (!this.socket && this.reconnectTimer === null) this.connect();
	};

	snapshot(): WebClientSnapshot {
		return {
			protocol: this.state,
			status: this.state.lastError
				? { message: this.state.lastError, isError: true }
				: this.status,
			submitting: this.submitting,
			loadingEarlier: this.loadingEarlier,
			answeringInteractionId: this.answeringInteractionId,
			attachments: [...this.pendingAttachments],
			interactionHydrationErrors: new Map(this.interactionHydrationErrors),
			interactionResponseErrors: new Map(this.interactionResponseErrors),
		};
	}

	subscribe(listener: (snapshot: WebClientSnapshot) => void): () => void {
		this.listeners.add(listener);
		listener(this.snapshot());
		return () => this.listeners.delete(listener);
	}

	start(): void {
		if (this.started) return;
		this.started = true;
		window.addEventListener("online", this.onlineListener);
		this.connect();
	}

	dispose(): void {
		this.started = false;
		window.removeEventListener("online", this.onlineListener);
		if (this.reconnectTimer !== null) clearTimeout(this.reconnectTimer);
		if (this.protocolTimer !== null) clearTimeout(this.protocolTimer);
		this.reconnectTimer = null;
		this.protocolTimer = null;
		this.socket?.close();
		this.socket = null;
		this.rejectPendingCommands(new Error("Web client disposed"));
		this.listeners.clear();
	}

	isStreaming(): boolean {
		return this.state.serverState.isStreaming === true;
	}

	addAttachments(files: File[]): void {
		if (this.submitting || files.length === 0) return;
		this.pendingAttachments = [
			...this.pendingAttachments,
			...files.map((file) => ({ file })),
		];
		this.setStatus(`${this.pendingAttachments.length} attachments selected`);
		this.notify();
	}

	async removeAttachment(attachment: PendingAttachment): Promise<void> {
		this.pendingAttachments = this.pendingAttachments.filter(
			(item) => item !== attachment,
		);
		this.setStatus(
			this.pendingAttachments.length === 0
				? "Attachment removed"
				: `${this.pendingAttachments.length} attachments selected`,
		);
		this.notify();
		if (!attachment.id) return;
		try {
			const response = await fetch(
				`/api/attachments/${encodeURIComponent(attachment.id)}`,
				{ method: "DELETE" },
			);
			if (!response.ok && response.status !== 404) {
				throw new Error(`Attachment removal failed (${response.status})`);
			}
		} catch (error) {
			this.reportError(error);
		}
	}

	async submit(messageValue: string): Promise<boolean> {
		if (this.submitting) return false;
		const message = messageValue.trim();
		if (!message && this.pendingAttachments.length === 0) return false;
		const submittedAttachments = [...this.pendingAttachments];
		this.submitting = true;
		this.setStatus(
			submittedAttachments.length > 0 ? "Uploading attachments…" : "Sending…",
		);
		this.notify();
		try {
			const attachmentIds: string[] = [];
			for (const attachment of submittedAttachments) {
				attachmentIds.push(await this.uploadAttachment(attachment));
			}
			await this.sendCommand({
				type: "prompt",
				message,
				...(attachmentIds.length > 0 ? { attachmentIds } : {}),
				...(this.isStreaming() ? { streamingBehavior: "followUp" } : {}),
			});
			this.pendingAttachments = this.pendingAttachments.filter(
				(attachment) => !submittedAttachments.includes(attachment),
			);
			this.setStatus("");
			return true;
		} catch (error) {
			this.reportError(error);
			return false;
		} finally {
			this.submitting = false;
			this.notify();
		}
	}

	reportComposerUnavailable(): void {
		this.setStatus(
			this.submitting ? "Still sending…" : "Kit is not connected",
			true,
		);
		this.notify();
	}

	async abort(): Promise<void> {
		try {
			await this.sendCommand({ type: "abort" });
		} catch (error) {
			this.reportError(error);
		}
	}

	async answerInteraction(requestId: string, response: unknown): Promise<void> {
		if (this.answeringInteractionId) return;
		this.interactionResponseErrors.delete(requestId);
		this.answeringInteractionId = requestId;
		this.notify();
		try {
			await this.sendCommand({
				type: "ui_response",
				requestId,
				response,
			});
		} catch (error) {
			this.answeringInteractionId = null;
			const message = error instanceof Error ? error.message : String(error);
			this.interactionResponseErrors.set(requestId, message);
			this.reportError(error);
			this.notify();
		}
	}

	ensureInteractionHydrated(requestId: string): void {
		void this.hydrateInteraction(requestId);
	}

	retryInteraction(requestId: string): void {
		this.interactionHydrationErrors.delete(requestId);
		this.notify();
		this.ensureInteractionHydrated(requestId);
	}

	async loadEarlier(beforeCommit?: () => void): Promise<void> {
		if (this.loadingEarlier || this.state.messageOffset === 0) return;
		this.loadingEarlier = true;
		this.notify();
		try {
			const oldOffset = this.state.messageOffset;
			const targetOffset = Math.max(0, oldOffset - 50);
			const messages: unknown[] = [];
			let cursor = targetOffset;
			let totalMessageCount = this.state.totalMessageCount;
			while (cursor < oldOffset) {
				const response = await this.sendCommand({
					type: "get_messages",
					offset: cursor,
					limit: Math.min(50, oldOffset - cursor),
				});
				if (
					!isRecord(response.data) ||
					!Array.isArray(response.data.messages)
				) {
					throw new Error("Invalid message page response");
				}
				if (
					typeof response.data.offset === "number" &&
					response.data.offset !== cursor
				) {
					throw new Error("Message page is not contiguous");
				}
				if (response.data.messages.length === 0) {
					throw new Error("Message history ended before the requested cursor");
				}
				messages.push(
					...(await Promise.all(
						response.data.messages.map((message) =>
							this.resolveMessageReference(message),
						),
					)),
				);
				cursor += response.data.messages.length;
				totalMessageCount =
					typeof response.data.totalMessageCount === "number"
						? response.data.totalMessageCount
						: totalMessageCount;
			}
			beforeCommit?.();
			this.state = prependMessages(
				this.state,
				messages,
				targetOffset,
				totalMessageCount,
			);
			this.setStatus("");
		} catch (error) {
			this.reportError(error);
		} finally {
			this.loadingEarlier = false;
			this.notify();
		}
	}

	private notify(): void {
		const snapshot = this.snapshot();
		for (const listener of this.listeners) listener(snapshot);
	}

	private setStatus(message: string, isError = false): void {
		this.status = { message, isError };
	}

	private reportError(error: unknown): void {
		this.setStatus(
			error instanceof Error ? error.message : String(error),
			true,
		);
		this.notify();
	}

	private rejectPendingCommands(error: Error): void {
		for (const pending of this.pendingCommands.values()) pending.reject(error);
		this.pendingCommands.clear();
	}

	private reconnectUrl(): string {
		const url = new URL("/api/rpc", window.location.href);
		url.protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
		if (!this.requireSnapshot && this.state.streamId) {
			url.searchParams.set("streamId", this.state.streamId);
			url.searchParams.set("after", String(this.state.sequence));
		}
		return url.href;
	}

	private scheduleReconnect(): void {
		if (!this.started || this.reconnectTimer !== null) return;
		const delay = Math.min(500 * 2 ** this.reconnectAttempt, 10_000);
		this.reconnectAttempt += 1;
		this.reconnectTimer = window.setTimeout(() => {
			this.reconnectTimer = null;
			this.connect();
		}, delay);
	}

	private messageDelta(record: unknown): Record<string, unknown> | null {
		if (
			!isRecord(record) ||
			record.type !== "message_update" ||
			!isRecord(record.assistantMessageEvent)
		) {
			return null;
		}
		const event = record.assistantMessageEvent;
		return (event.type === "text_delta" || event.type === "thinking_delta") &&
			typeof event.delta === "string" &&
			typeof event.contentIndex === "number"
			? event
			: null;
	}

	private flushProtocolRecords(): void {
		if (this.protocolTimer !== null) clearTimeout(this.protocolTimer);
		this.protocolTimer = null;
		try {
			const records = this.queuedRecords.splice(0);
			for (let index = 0; index < records.length; index += 1) {
				let record = records[index];
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
							next.sequence !== lastSequence + 1 ||
							nextDelta.type !== delta.type ||
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
							assistantMessageEvent: { ...delta, delta: combinedDelta },
						};
						this.state = reduceClientRecord(this.state, record);
						this.state = { ...this.state, sequence: lastSequence };
						continue;
					}
				}
				if (isRecord(record) && record.type === "sync") {
					this.activeSyncMode =
						typeof record.mode === "string" ? record.mode : null;
				}
				if (isRecord(record) && record.type === "resync_required") {
					throw new ProtocolSyncError("The session requires a fresh snapshot");
				}
				this.state = reduceClientRecord(this.state, record);
				if (isRecord(record) && record.type === "sync_complete") {
					if (this.activeSyncMode === "snapshot") this.requireSnapshot = false;
					this.activeSyncMode = null;
				}
			}
			for (const requestId of this.interactionResponseErrors.keys()) {
				if (
					!this.state.pendingInteractions.some(
						(request) => isRecord(request) && request.id === requestId,
					)
				) {
					this.interactionResponseErrors.delete(requestId);
				}
			}
			if (
				this.answeringInteractionId &&
				!this.state.pendingInteractions.some(
					(request) =>
						isRecord(request) && request.id === this.answeringInteractionId,
				)
			) {
				this.answeringInteractionId = null;
			}
			if (this.state.phase === "live") {
				this.reconnectAttempt = 0;
				void this.loadPendingInteractions();
				void this.hydrateVisibleMessageReferences();
			}
			this.notify();
		} catch (error) {
			this.requireSnapshot = true;
			this.reportError(error);
			this.socket?.close();
		}
	}

	private enqueueProtocolRecord(record: unknown): void {
		this.queuedRecords.push(record);
		if (this.queuedRecords.length >= 256) {
			this.flushProtocolRecords();
			return;
		}
		if (this.protocolTimer === null) {
			this.protocolTimer = window.setTimeout(
				() => this.flushProtocolRecords(),
				0,
			);
		}
	}

	private connect(): void {
		if (!this.started) return;
		this.state = withConnectionPhase(this.state, "connecting");
		this.notify();
		const nextSocket = new WebSocket(this.reconnectUrl());
		this.socket = nextSocket;
		nextSocket.addEventListener("message", (event) => {
			if (this.socket !== nextSocket) return;
			try {
				const record: unknown = JSON.parse(String(event.data));
				if (isRecord(record) && record.type === "response") {
					if (this.queuedRecords.length > 0) this.flushProtocolRecords();
					const id = record.id;
					if (typeof id === "string") {
						const pending = this.pendingCommands.get(id);
						if (pending) {
							this.pendingCommands.delete(id);
							if (record.success === true) pending.resolve(record);
							else {
								pending.reject(
									new Error(
										typeof record.error === "string"
											? record.error
											: "Command failed",
									),
								);
							}
						}
					}
					return;
				}
				this.enqueueProtocolRecord(record);
			} catch (error) {
				this.requireSnapshot = true;
				this.reportError(error);
				nextSocket.close();
			}
		});
		nextSocket.addEventListener("close", () => {
			if (this.socket !== nextSocket) return;
			this.socket = null;
			this.queuedRecords.length = 0;
			if (this.protocolTimer !== null) clearTimeout(this.protocolTimer);
			this.protocolTimer = null;
			this.state = withConnectionPhase(this.state, "disconnected");
			this.rejectPendingCommands(
				new Error("Connection closed before a response arrived"),
			);
			this.notify();
			this.scheduleReconnect();
		});
		nextSocket.addEventListener("error", () => {
			this.setStatus("Unable to connect to the Kit session.", true);
			this.notify();
		});
	}

	private sendCommand(
		command: Record<string, unknown>,
	): Promise<Record<string, unknown>> {
		if (
			!this.socket ||
			this.socket.readyState !== WebSocket.OPEN ||
			this.state.phase !== "live"
		) {
			return Promise.reject(new Error("Kit is not connected"));
		}
		this.commandCounter += 1;
		const id = `web-${Date.now().toString(36)}-${this.commandCounter.toString(36)}`;
		return new Promise((resolve, reject) => {
			this.pendingCommands.set(id, { resolve, reject });
			this.socket?.send(JSON.stringify({ ...command, id }));
		});
	}

	private async uploadAttachment(
		attachment: PendingAttachment,
	): Promise<string> {
		if (attachment.id) return attachment.id;
		const form = new FormData();
		form.append("file", attachment.file);
		const response = await fetch("/api/attachments", {
			method: "POST",
			body: form,
		});
		const payload: unknown = await response.json();
		if (!response.ok || !isRecord(payload) || !isRecord(payload.attachment)) {
			throw new Error(
				isRecord(payload) && typeof payload.error === "string"
					? payload.error
					: `Attachment upload failed (${response.status})`,
			);
		}
		if (typeof payload.attachment.id !== "string") {
			throw new Error("Attachment upload returned no id");
		}
		attachment.id = payload.attachment.id;
		return attachment.id;
	}

	private async resolveMessageReference(message: unknown): Promise<unknown> {
		if (
			!isRecord(message) ||
			message.type !== "message_reference" ||
			typeof message.token !== "string"
		) {
			return message;
		}
		const chunks: Uint8Array[] = [];
		let offset = 0;
		let complete = false;
		while (!complete) {
			const response = await this.sendCommand({
				type: "get_message_chunk",
				token: message.token,
				offset,
			});
			if (!isRecord(response.data) || typeof response.data.data !== "string") {
				throw new Error("Invalid message chunk response");
			}
			const binary = atob(response.data.data);
			const bytes = Uint8Array.from(binary, (character) =>
				character.charCodeAt(0),
			);
			chunks.push(bytes);
			offset =
				typeof response.data.nextOffset === "number"
					? response.data.nextOffset
					: offset + bytes.length;
			complete = response.data.complete === true;
		}
		const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
		const joined = new Uint8Array(total);
		let cursor = 0;
		for (const chunk of chunks) {
			joined.set(chunk, cursor);
			cursor += chunk.length;
		}
		return JSON.parse(new TextDecoder().decode(joined));
	}

	private async recoverMessageReference(
		message: Record<string, unknown>,
		messageIndex: number,
	): Promise<RecoveredMessage> {
		let candidate: unknown = message;
		let lastError: unknown = null;
		let rebased = false;
		for (let attempt = 0; attempt < 3; attempt += 1) {
			try {
				return {
					message: await this.resolveMessageReference(candidate),
					rebased,
				};
			} catch (error) {
				lastError = error;
				if (this.state.phase !== "live") throw error;
				const response = await this.sendCommand({
					type: "get_messages",
					offset: messageIndex,
					limit: 1,
				});
				if (
					!isRecord(response.data) ||
					response.data.offset !== messageIndex ||
					!Array.isArray(response.data.messages) ||
					response.data.messages.length !== 1
				) {
					throw new Error("Message recovery returned the wrong record");
				}
				candidate = response.data.messages[0];
				rebased = true;
				if (!isRecord(candidate) || candidate.type !== "message_reference") {
					return { message: candidate, rebased };
				}
			}
		}
		return {
			message: {
				type: "message_unavailable",
				role: message.role,
				messageIndex,
				reason:
					lastError instanceof Error ? lastError.message : "recovery_failed",
			},
			rebased: true,
		};
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
				const recovered = await this.recoverMessageReference(
					message,
					messageIndex,
				);
				const currentIndex = this.state.messages.indexOf(message);
				if (currentIndex < 0) continue;
				this.state = hydrateMessageReference(
					this.state,
					currentIndex,
					message,
					recovered.message,
					!recovered.rebased,
				);
				this.notify();
			} catch (error) {
				this.reportError(error);
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
		const revision = this.state.interactionRevision;
		try {
			let offset = 0;
			let total = this.state.totalPendingInteractionCount;
			const requests: unknown[] = [];
			while (offset < total) {
				const response = await this.sendCommand({
					type: "get_pending_interactions",
					offset,
					limit: Math.min(20, total - offset),
				});
				if (this.state.interactionRevision !== revision) return;
				if (
					!isRecord(response.data) ||
					!Array.isArray(response.data.requests)
				) {
					throw new Error("Invalid pending interaction page response");
				}
				if (
					typeof response.data.offset === "number" &&
					response.data.offset !== offset
				) {
					throw new Error("Pending interaction page is not contiguous");
				}
				if (response.data.requests.length === 0) break;
				for (const request of response.data.requests) {
					if (
						!isRecord(request) ||
						typeof request.id !== "string" ||
						requests.some(
							(existing) => isRecord(existing) && existing.id === request.id,
						)
					) {
						continue;
					}
					requests.push(request);
				}
				offset += response.data.requests.length;
				total =
					typeof response.data.totalRequestCount === "number"
						? response.data.totalRequestCount
						: total;
			}
			if (this.state.interactionRevision !== revision) return;
			this.state = {
				...this.state,
				pendingInteractions: requests,
				pendingInteractionOffset: 0,
				totalPendingInteractionCount: total,
			};
			this.notify();
		} catch (error) {
			this.reportError(error);
		} finally {
			this.loadingPendingInteractions = false;
			if (
				this.state.phase === "live" &&
				this.state.interactionRevision !== revision &&
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
		)
			return;
		this.hydratingInteractions.add(requestId);
		try {
			const chunks: Uint8Array[] = [];
			let offset = 0;
			let complete = false;
			while (!complete) {
				const response = await this.sendCommand({
					type: "get_pending_interaction_chunk",
					requestId,
					offset,
				});
				if (
					!isRecord(response.data) ||
					typeof response.data.data !== "string"
				) {
					throw new Error("Invalid interaction chunk response");
				}
				const totalBytes = response.data.totalBytes;
				if (
					typeof totalBytes !== "number" ||
					!Number.isSafeInteger(totalBytes) ||
					totalBytes < 0 ||
					totalBytes > MAX_INTERACTION_BYTES ||
					response.data.offset !== offset
				) {
					throw new Error(
						"Interaction payload exceeds the client recovery limit",
					);
				}
				const binary = atob(response.data.data);
				const bytes = Uint8Array.from(binary, (character) =>
					character.charCodeAt(0),
				);
				chunks.push(bytes);
				const nextOffset = response.data.nextOffset;
				if (
					typeof nextOffset !== "number" ||
					!Number.isSafeInteger(nextOffset) ||
					nextOffset !== offset + bytes.length ||
					nextOffset > totalBytes ||
					(nextOffset === offset && response.data.complete !== true)
				) {
					throw new Error("Interaction chunks are not contiguous");
				}
				offset = nextOffset;
				complete = response.data.complete === true;
				if (complete && offset !== totalBytes) {
					throw new Error("Interaction payload ended at the wrong offset");
				}
			}
			const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
			const joined = new Uint8Array(total);
			let cursor = 0;
			for (const chunk of chunks) {
				joined.set(chunk, cursor);
				cursor += chunk.length;
			}
			const hydrated: unknown = JSON.parse(new TextDecoder().decode(joined));
			if (!isRecord(hydrated) || hydrated.id !== requestId) {
				throw new Error("Interaction recovery returned the wrong request");
			}
			this.state = {
				...this.state,
				pendingInteractions: this.state.pendingInteractions.map((request) =>
					isRecord(request) && request.id === requestId ? hydrated : request,
				),
			};
			this.interactionHydrationErrors.delete(requestId);
		} catch (error) {
			const message = error instanceof Error ? error.message : String(error);
			this.interactionHydrationErrors.set(requestId, message);
			this.setStatus(message, true);
		} finally {
			this.hydratingInteractions.delete(requestId);
			this.notify();
		}
	}
}
