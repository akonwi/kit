export type SessionServiceUnsubscribe = () => void;

export interface SessionServiceStateSource {
	getSnapshot(): SessionServiceSnapshot;
	subscribe(
		listener: (snapshot: SessionServiceSnapshot) => void,
	): SessionServiceUnsubscribe;
}

export type SessionServiceAvailableModel = Readonly<{
	id: string;
	provider: string;
	name?: string;
}>;

export type SessionServiceModelSelection = Readonly<{
	provider: string;
	modelId: string;
	thinkingLevel?: string;
	contextUsage?: SessionServiceContextUsage;
}>;

export type SessionServiceContextUsage = Readonly<{
	tokens: number;
	contextWindow: number;
	percent: number;
}>;

export type SessionServiceTextPart = Readonly<{
	type: "text";
	text: string;
}>;

export type SessionServiceThinkingPart = Readonly<{
	type: "thinking";
	thinking: string;
}>;

export type SessionServiceImagePart = Readonly<{
	type: "image";
	data: string;
	mimeType: string;
	filename?: string;
	attachmentId?: string;
}>;

export type SessionServiceToolCallPart = Readonly<{
	type: "toolCall";
	id: string;
	name: string;
	arguments: Readonly<Record<string, unknown>>;
}>;

export type SessionServiceReviewPart = Readonly<{
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

export type SessionServiceSyntheticMetadata = Readonly<{
	kind: "compaction-summary" | "handoff-summary" | "subagent-delegation";
	sourceSessionName?: string;
	subagentName?: string;
	subagentDescription?: string;
	subagentPrompt?: string;
	subagentSource?: "manual" | "agent";
}>;

export type SessionServiceMessage =
	| Readonly<{
			role: "user";
			messageId: string;
			turnId: string;
			timestamp: number;
			synthetic?: SessionServiceSyntheticMetadata;
			content:
				| string
				| readonly (
						| SessionServiceTextPart
						| SessionServiceImagePart
						| SessionServiceReviewPart
				  )[];
	  }>
	| Readonly<{
			role: "assistant";
			messageId: string;
			turnId: string;
			timestamp: number;
			content: readonly (
				| SessionServiceTextPart
				| SessionServiceThinkingPart
				| SessionServiceToolCallPart
			)[];
			stopReason:
				| "stop"
				| "length"
				| "toolUse"
				| "error"
				| "aborted"
				| "pending";
			errorMessage?: string;
			synthetic?: SessionServiceSyntheticMetadata;
	  }>
	| Readonly<{
			role: "toolResult";
			messageId: string;
			turnId: string;
			timestamp: number;
			toolCallId: string;
			toolName: string;
			content: readonly (SessionServiceTextPart | SessionServiceImagePart)[];
			isError: boolean;
			synthetic?: SessionServiceSyntheticMetadata;
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
			synthetic?: SessionServiceSyntheticMetadata;
	  }>;

export type SessionServiceQuestion = Readonly<{
	id: string;
	label: string;
	help?: string;
	kind: "text" | "select" | "multiselect" | "boolean";
	required: boolean;
	options?: readonly string[];
	placeholder?: string;
}>;

export type SessionServiceInteraction =
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
				questions: readonly SessionServiceQuestion[];
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

export type SessionServiceInteractionResponseByKind = {
	confirm: Readonly<{ confirmed: boolean }>;
	input: Readonly<{ value: string | null }>;
	select: Readonly<{ optionId: string | null }>;
	open_url: Readonly<{ opened: boolean }>;
	guided_questions: Readonly<{
		cancelled: boolean;
		answers: Readonly<Record<string, string | readonly string[] | boolean>>;
	}>;
};

export type SessionServiceInteractionKind =
	keyof SessionServiceInteractionResponseByKind;
export type SessionServiceInteractionForKind<
	K extends SessionServiceInteractionKind,
> = Extract<SessionServiceInteraction, { kind: K }>;

export type SessionServiceTool = Readonly<{
	id: string;
	turnId: string;
	name: string;
	args?: Readonly<Record<string, unknown>>;
	partialResult?: unknown;
	result?: unknown;
	isError: boolean;
	status: "running" | "complete";
}>;

export type SessionServiceSnapshot = Readonly<{
	session: Readonly<{
		id: string;
		name?: string;
		cwd: string;
		persistent: boolean;
	}>;
	agent: Readonly<{
		status: "idle" | "running" | "retrying" | "aborting";
		activeTurnId?: string;
		tools: readonly SessionServiceTool[];
	}>;
	transcript: Readonly<{
		messages: readonly SessionServiceMessage[];
		activeMessage?: SessionServiceMessage;
		offset: number;
		totalCount: number;
	}>;
	queue: Readonly<{
		generation: number;
		count: number;
		previews: readonly string[];
	}>;
	model: SessionServiceModelSelection;
	interactions: Readonly<{
		generation: number;
		pending: readonly SessionServiceInteraction[];
	}>;
}>;

export type SessionServiceToast = Readonly<{
	title: string;
	subtitle?: string;
	variant: "info" | "warning" | "error";
	persistent?: boolean;
}>;

export type SessionServiceEffect =
	| Readonly<{ type: "toast"; toast: SessionServiceToast }>
	| Readonly<{ type: "open-url"; url: string }>
	| Readonly<{ type: "notification"; title: string; message: string }>;

export interface SessionServiceEffectSource {
	subscribe(
		listener: (effect: SessionServiceEffect) => void,
	): SessionServiceUnsubscribe;
}

export interface SessionChatService {
	prompt(input: {
		message: string;
		attachmentIds?: readonly string[];
	}): Promise<void>;
	steer(message: string): Promise<void>;
	followUp(message: string): Promise<void>;
	abort(): Promise<void>;
}

export interface SessionTranscriptService {
	getMessages(input: { offset: number; limit: number }): Promise<
		Readonly<{
			messages: readonly SessionServiceMessage[];
			offset: number;
			totalCount: number;
		}>
	>;
}

export interface SessionModelService {
	list(): Promise<readonly SessionServiceAvailableModel[]>;
	select(provider: string, modelId: string): Promise<void>;
	listThinkingLevels(): Promise<readonly string[]>;
	setThinkingLevel(level: string): Promise<void>;
}

export interface SessionInteractionService {
	respond<K extends SessionServiceInteractionKind>(
		interaction: SessionServiceInteractionForKind<K>,
		response: SessionServiceInteractionResponseByKind[K],
	): Promise<void>;
}

export type SessionServiceCommandDescriptor = Readonly<{
	id: string;
	name: string;
	description?: string;
	argName?: string;
	category?: string;
}>;

export interface SessionCommandService {
	list(): Promise<
		Readonly<{
			commands: readonly SessionServiceCommandDescriptor[];
			registryGeneration: number;
		}>
	>;
	/** A bound service must not expose commands that replace its session. */
	execute(input: {
		commandId: string;
		args?: string;
		registryGeneration: number;
	}): Promise<void>;
}

export type SessionServiceAttachmentSource = Readonly<{
	name: string;
	mimeType?: string;
	size: number;
	bytes(): AsyncIterable<Uint8Array>;
}>;

export type SessionServiceStagedAttachment = Readonly<{
	id: string;
	name: string;
	mimeType?: string;
	size: number;
}>;

export type SessionServiceAttachmentContent = Readonly<{
	id: string;
	name?: string;
	mimeType?: string;
	size?: number;
	bytes(): AsyncIterable<Uint8Array>;
}>;

export interface SessionAttachmentService {
	readonly constraints: Readonly<{
		maxFilesPerPrompt: number;
		maxFileBytes: number;
		maxPromptBytes: number;
	}>;
	stage(
		source: SessionServiceAttachmentSource,
	): Promise<SessionServiceStagedAttachment>;
	read(id: string): Promise<SessionServiceAttachmentContent>;
	remove(id: string): Promise<void>;
}

/** Authoritative application service for exactly one running session. */
export interface SessionService {
	readonly sessionId: string;
	readonly state: SessionServiceStateSource;
	readonly effects: SessionServiceEffectSource;
	readonly chat: SessionChatService;
	readonly transcript: SessionTranscriptService;
	readonly models: SessionModelService;
	readonly interactions: SessionInteractionService;
	readonly commands: SessionCommandService;
	readonly attachments?: SessionAttachmentService;

	/** Idempotent and terminal; the host owns when this service is disposed. */
	dispose(): Promise<void>;
}
