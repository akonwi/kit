export type Unsubscribe = () => void;

export interface ObservableState<T> {
	getSnapshot(): T;
	subscribe(listener: (snapshot: T) => void): Unsubscribe;
}

export type AvailableModel = Readonly<{
	id: string;
	provider: string;
	name?: string;
}>;

export type SessionModelSelection = Readonly<{
	provider: string;
	modelId: string;
	thinkingLevel?: string;
	contextUsage?: SessionContextUsage;
}>;

export type SessionContextUsage = Readonly<{
	tokens: number;
	contextWindow: number;
	percent: number;
}>;

export type SessionClientTextPart = Readonly<{
	type: "text";
	text: string;
}>;

export type SessionClientThinkingPart = Readonly<{
	type: "thinking";
	thinking: string;
}>;

export type SessionClientImagePart =
	| Readonly<{
			type: "image";
			data: string;
			dataOmitted?: false;
			mimeType: string;
			filename?: string;
			attachmentId?: string;
	  }>
	| Readonly<{
			type: "image";
			dataOmitted: true;
			mimeType: string;
			filename?: string;
			attachmentId?: string;
	  }>;

export type SessionClientToolCallPart = Readonly<{
	type: "toolCall";
	id: string;
	name: string;
	arguments: Readonly<Record<string, unknown>>;
}>;

export type SessionClientReviewPart = Readonly<{
	type: "code-review";
	review: Readonly<{
		submittedAt: string;
		source?: "file";
		commit?: Readonly<{
			sha: string;
			parentSha: string;
			subject: string;
		}>;
		submissionId?: string;
		submissionFingerprint?: string;
		files: readonly Readonly<{
			path: string;
			fileComment: string;
			ranges: readonly Readonly<{
				side: "additions" | "deletions";
				startLine: number;
				endLine: number;
				comment: string;
			}>[];
		}>[];
	}>;
}>;

export type SessionClientSyntheticMetadata = Readonly<{
	kind: "compaction-summary" | "handoff-summary" | "subagent-delegation";
	sourceSessionName?: string;
	subagentName?: string;
	subagentDescription?: string;
	subagentPrompt?: string;
	subagentSource?: "manual" | "agent";
}>;

export type SessionClientMessage =
	| Readonly<{
			role: "user";
			messageId: string;
			turnId: string;
			timestamp: number;
			synthetic?: SessionClientSyntheticMetadata;
			content:
				| string
				| readonly (
						| SessionClientTextPart
						| SessionClientImagePart
						| SessionClientReviewPart
				  )[];
	  }>
	| Readonly<{
			role: "assistant";
			messageId: string;
			turnId: string;
			timestamp: number;
			content: readonly (
				| SessionClientTextPart
				| SessionClientThinkingPart
				| SessionClientToolCallPart
			)[];
			stopReason:
				| "stop"
				| "length"
				| "toolUse"
				| "error"
				| "aborted"
				| "pending";
			errorMessage?: string;
			synthetic?: SessionClientSyntheticMetadata;
	  }>
	| Readonly<{
			role: "toolResult";
			messageId: string;
			turnId: string;
			timestamp: number;
			toolCallId: string;
			toolName: string;
			content: readonly (SessionClientTextPart | SessionClientImagePart)[];
			isError: boolean;
			synthetic?: SessionClientSyntheticMetadata;
	  }>
	| Readonly<{
			role: "bashExecution";
			messageId: string;
			turnId: string;
			timestamp: number;
			command: string;
			output?: string;
			exitCode?: number;
			cancelled?: boolean;
			truncated?: boolean;
			synthetic?: SessionClientSyntheticMetadata;
	  }>
	| Readonly<{
			type: "message_unavailable";
			role?: "user" | "assistant" | "toolResult" | "bashExecution";
			messageId?: string;
			turnId?: string;
			messageIndex: number;
			serializedBytes?: number;
			reason: string;
	  }>;

export type SessionClientTool = Readonly<{
	id: string;
	turnId: string;
	name: string;
	args?: Readonly<Record<string, unknown>>;
	partialResult?: unknown;
	result?: unknown;
	isError: boolean;
	status: "running" | "complete";
}>;

export type SessionClientQuestion = Readonly<{
	id: string;
	label: string;
	help?: string;
	kind: "text" | "select" | "multiselect" | "boolean";
	required: boolean;
	options?: readonly string[];
	placeholder?: string;
}>;

export type SessionClientInteraction =
	| Readonly<{
			id: string;
			kind: "confirm";
			createdAt: string;
			payload: Readonly<{
				title: string;
				message?: string;
				confirmLabel?: string;
				cancelLabel?: string;
				defaultValue?: boolean;
			}>;
	  }>
	| Readonly<{
			id: string;
			kind: "input";
			createdAt: string;
			payload: Readonly<{
				title: string;
				message?: string;
				placeholder?: string;
				initialValue?: string;
			}>;
	  }>
	| Readonly<{
			id: string;
			kind: "select";
			createdAt: string;
			payload: Readonly<{
				title: string;
				message?: string;
				filterable?: boolean;
				placeholder?: string;
				options: readonly Readonly<{
					id: string;
					label: string;
					description?: string;
				}>[];
			}>;
	  }>
	| Readonly<{
			id: string;
			kind: "guided_questions";
			createdAt: string;
			payload: Readonly<{
				title?: string;
				intro?: string;
				questions: readonly SessionClientQuestion[];
			}>;
	  }>
	| Readonly<{
			id: string;
			kind: "open_url";
			createdAt: string;
			payload: Readonly<{
				title: string;
				url: string;
				source?: string;
			}>;
	  }>;

export type SessionInteractionResponseByKind = {
	confirm: Readonly<{ confirmed: boolean }>;
	input: Readonly<{ value: string | null }>;
	select: Readonly<{ optionId: string | null }>;
	open_url: Readonly<{ opened: boolean }>;
	guided_questions: Readonly<{
		cancelled: boolean;
		answers: Readonly<Record<string, string | readonly string[] | boolean>>;
	}>;
};

export type SessionInteractionKind = keyof SessionInteractionResponseByKind;
export type SessionInteractionForKind<K extends SessionInteractionKind> =
	Extract<SessionClientInteraction, { kind: K }>;

export type SessionCommandDescriptor = Readonly<{
	id: string;
	name: string;
	description?: string;
	argName?: string;
	category?: string;
}>;

export type SessionCommandList = Readonly<{
	commands: readonly SessionCommandDescriptor[];
	registryGeneration: number;
}>;

export type SessionClientSnapshot = Readonly<{
	connection: Readonly<{
		phase: "disconnected" | "connecting" | "synchronizing" | "live";
		error?: string;
	}>;
	session: Readonly<{
		id: string;
		name?: string;
		cwd: string;
		persistent: boolean;
	}>;
	agent: Readonly<{
		status: "idle" | "running" | "retrying" | "aborting";
		activeTurnId?: string;
		tools: readonly SessionClientTool[];
	}>;
	transcript: Readonly<{
		messages: readonly SessionClientMessage[];
		activeMessage?: SessionClientMessage;
		offset: number;
		totalCount: number;
	}>;
	queue: Readonly<{
		generation: number;
		count: number;
		previews: readonly string[];
	}>;
	model: SessionModelSelection;
	interactions: Readonly<{
		generation: number;
		pending: readonly SessionClientInteraction[];
	}>;
}>;

export type SessionClientToast = Readonly<{
	title: string;
	subtitle?: string;
	variant: "info" | "warning" | "error";
	persistent?: boolean;
}>;

export type SessionClientEffect =
	| Readonly<{ type: "toast"; toast: SessionClientToast }>
	| Readonly<{ type: "open-url"; url: string }>
	| Readonly<{ type: "notification"; title: string; message: string }>
	| Readonly<{ type: "resynchronized"; reason: string }>;

export interface ClientEffectSource {
	subscribe(listener: (effect: SessionClientEffect) => void): Unsubscribe;
}

export type AttachmentSource = Readonly<{
	name: string;
	mimeType?: string;
	size: number;
	bytes(): AsyncIterable<Uint8Array>;
}>;

export type StagedAttachment = Readonly<{
	id: string;
	name: string;
	mimeType?: string;
	size: number;
}>;

export type AttachmentContent = Readonly<{
	id: string;
	name?: string;
	mimeType?: string;
	size?: number;
	bytes(): AsyncIterable<Uint8Array>;
}>;

export interface AttachmentClient {
	readonly constraints: Readonly<{
		maxFilesPerPrompt: number;
		maxFileBytes: number;
		maxPromptBytes: number;
	}>;
	stage(source: AttachmentSource): Promise<StagedAttachment>;
	read(id: string): Promise<AttachmentContent>;
	remove(id: string): Promise<void>;
}

export type PromptInput = Readonly<{
	message: string;
	attachmentIds?: readonly string[];
}>;

export interface ChatClient {
	prompt(input: PromptInput): Promise<void>;
	steer(message: string): Promise<void>;
	followUp(message: string): Promise<void>;
	abort(): Promise<void>;
}

export type TranscriptClientSnapshot = Readonly<{
	loadingEarlier: boolean;
}>;

export interface TranscriptClient {
	readonly state: ObservableState<TranscriptClientSnapshot>;
	loadEarlier(): Promise<void>;
}

export interface ModelClient {
	list(): Promise<readonly AvailableModel[]>;
	select(provider: string, modelId: string): Promise<void>;
	listThinkingLevels(): Promise<readonly string[]>;
	setThinkingLevel(level: string): Promise<void>;
}

export interface InteractionClient {
	respond<K extends SessionInteractionKind>(
		interaction: SessionInteractionForKind<K>,
		response: SessionInteractionResponseByKind[K],
	): Promise<void>;
}

export interface CommandClient {
	list(): Promise<SessionCommandList>;
	/** Bound clients must not expose commands that replace their session. */
	execute(input: {
		commandId: string;
		args?: string;
		registryGeneration: number;
	}): Promise<void>;
}

/**
 * A negotiated, immutable binding to one authoritative running session.
 *
 * This initial core surface includes attachments because they participate in
 * prompt submission. Additional typed optional feature facets are added as
 * their TUI workflows migrate; every facet must remain stable for this
 * client's lifetime.
 */
export interface SessionClient {
	readonly sessionId: string;
	readonly state: ObservableState<SessionClientSnapshot>;
	readonly effects: ClientEffectSource;
	readonly chat: ChatClient;
	readonly transcript: TranscriptClient;
	readonly models: ModelClient;
	readonly interactions: InteractionClient;
	readonly commands: CommandClient;
	/** Present only when attachment support was negotiated before attachment. */
	readonly attachments?: AttachmentClient;

	/**
	 * Idempotent and terminal. It releases this client binding only; an embedded
	 * adapter must not dispose the authoritative SessionService it wraps.
	 */
	dispose(): Promise<void>;
}

export type HostClientSnapshot = Readonly<{
	connection: Readonly<{
		phase: "disconnected" | "connecting" | "live";
		error?: string;
	}>;
}>;

export type SessionSummary = Readonly<{
	id: string;
	cwd: string;
	name?: string;
	model?: string;
	thinkingLevel?: string;
	createdAt: string;
	updatedAt: string;
	messageCount: number;
	firstMessage?: string;
	parentSessionId?: string;
	forkedFromTurnId?: string;
}>;

export type SessionListOptions = Readonly<{
	cwd?: string;
}>;

export type CreateSessionInput = Readonly<{
	cwd?: string;
	name?: string;
}>;

export interface KitHostClient {
	readonly state: ObservableState<HostClientSnapshot>;

	/** Idempotent; resolves only after host negotiation succeeds. */
	connect(): Promise<void>;
	/** Host operations reject while disconnected or after disposal. */
	listSessions(
		options?: SessionListOptions,
	): Promise<readonly SessionSummary[]>;
	createSession(input?: CreateSessionInput): Promise<SessionSummary>;
	/**
	 * Returns a stable-facet client only after negotiation and initial sync.
	 * Reconnection is automatic until that client is disposed.
	 */
	attachSession(sessionId: string): Promise<SessionClient>;
	/** Idempotent and terminal; also disposes clients created by this host client. */
	dispose(): Promise<void>;
}
