import type {
	RpcCommand,
	RpcConnectionSnapshot,
	RpcResponse,
} from "@akonwi/kit-protocol";
import { RemoteReviewService } from "../features/review/remote-service";
import {
	createSession,
	listAllSessions,
	listSessionsForCwd,
	readSession,
	type Session,
	type SessionSummary,
	toSummary,
	writeSession,
} from "../session";
import { createHeadlessHost } from "./headless-host";
import { RemoteAttachmentStore } from "./remote-attachment-store";
import { RemoteInteractionBroker } from "./remote-interaction-broker";
import {
	type RpcEventListener,
	RpcSessionHost,
	type RpcWriter,
} from "./rpc-session-host";

type SessionRpcHost = Pick<
	RpcSessionHost,
	| "abortAndWait"
	| "connectClient"
	| "dispose"
	| "getConnectionSnapshot"
	| "handleCommand"
	| "subscribe"
>;

type RunningSession = {
	rpc: SessionRpcHost;
	dispose: () => Promise<void>;
};

type KitServerStorage = {
	createSession: typeof createSession;
	listAllSessions: typeof listAllSessions;
	listSessionsForCwd: typeof listSessionsForCwd;
	readSession: typeof readSession;
	writeSession: typeof writeSession;
};

export type KitServerOptions = {
	storage?: KitServerStorage;
	startSession?: (
		session: Session,
		signal: AbortSignal,
	) => Promise<RunningSession>;
};

const defaultStorage: KitServerStorage = {
	createSession,
	listAllSessions,
	listSessionsForCwd,
	readSession,
	writeSession,
};

const SERVER_SCOPED_COMMANDS = new Set([
	"new_session",
	"list_sessions",
	"open_session",
	"switch_session",
]);

async function startSession(
	session: Session,
	signal: AbortSignal,
): Promise<RunningSession> {
	const interactions = new RemoteInteractionBroker();
	const attachments = new RemoteAttachmentStore();
	let headless: Awaited<ReturnType<typeof createHeadlessHost>> | null = null;
	let review: RemoteReviewService | null = null;
	let rpc: RpcSessionHost | null = null;
	try {
		headless = await createHeadlessHost(session, {
			persistSession: true,
			signal,
			interactions,
			externalPlugins: true,
			remoteChrome: true,
			remotePromptCommands: true,
		});
		review = new RemoteReviewService(headless.runtime);
		rpc = new RpcSessionHost(headless.runtime, {
			persistSessions: true,
			interactions,
			attachments,
			scratchpad: headless.scratchpad ?? undefined,
			review,
			commands: headless.commands,
			header: headless.header,
			footer: headless.footer,
			waitForWorkspaceReady: headless.waitForWorkspaceReady,
			reloadHost: headless.reload,
			allowLegacySessionPaths: false,
			allowBashExecution: true,
			sessionBinding: "fixed",
		});
	} catch (error) {
		rpc?.dispose();
		review?.dispose();
		interactions.dispose();
		attachments.dispose();
		await headless?.dispose().catch(() => {});
		if (signal.aborted && signal.reason instanceof Error) throw signal.reason;
		throw error;
	}

	let disposed = false;
	return {
		rpc,
		dispose: async () => {
			if (disposed) return;
			disposed = true;
			let cleanupError: unknown;
			try {
				await rpc.abortAndWait();
			} catch (error) {
				cleanupError = error;
			} finally {
				rpc.dispose();
				review.dispose();
				interactions.dispose();
				attachments.dispose();
			}
			try {
				await headless.dispose();
			} catch (error) {
				cleanupError ??= error;
			}
			if (cleanupError) throw cleanupError;
		},
	};
}

export class KitServerConnection {
	private closed = false;
	private readonly subscriptions = new Set<() => void>();

	constructor(
		readonly sessionId: string,
		private readonly running: RunningSession,
		private readonly release: (connection: KitServerConnection) => void,
	) {}

	subscribe(listener: RpcEventListener): () => void {
		this.assertOpen();
		const unsubscribeRpc = this.running.rpc.subscribe(listener);
		let unsubscribeClient: () => void;
		try {
			unsubscribeClient = this.running.rpc.connectClient(listener);
		} catch (error) {
			unsubscribeRpc();
			throw error;
		}
		let active = true;
		const unsubscribe = () => {
			if (!active) return;
			active = false;
			unsubscribeRpc();
			unsubscribeClient();
			this.subscriptions.delete(unsubscribe);
		};
		this.subscriptions.add(unsubscribe);
		return unsubscribe;
	}

	getConnectionSnapshot(maxMessages?: number): RpcConnectionSnapshot {
		this.assertOpen();
		return this.running.rpc.getConnectionSnapshot(maxMessages);
	}

	async handleCommand(command: RpcCommand, respond: RpcWriter): Promise<void> {
		this.assertOpen();
		if (SERVER_SCOPED_COMMANDS.has(command.type)) {
			const response: RpcResponse = {
				...(typeof command.id === "string" ? { id: command.id } : {}),
				type: "response",
				command: command.type,
				success: false,
				error: "Command is unavailable on a bound session",
			};
			await respond(response);
			return;
		}
		await this.running.rpc.handleCommand(command, respond);
	}

	close(): void {
		if (this.closed) return;
		this.closed = true;
		for (const unsubscribe of [...this.subscriptions]) unsubscribe();
		this.release(this);
	}

	private assertOpen(): void {
		if (this.closed) throw new Error("Server session connection is closed");
	}
}

class SessionStartupCancelled extends Error {
	constructor() {
		super("Kit server disposed during session startup");
	}
}

export class KitServer {
	private readonly storage: KitServerStorage;
	private readonly startSession: NonNullable<KitServerOptions["startSession"]>;
	private readonly startupAbort = new AbortController();
	private readonly sessions = new Map<string, RunningSession>();
	private readonly starting = new Map<string, Promise<RunningSession>>();
	private readonly mutations = new Set<Promise<unknown>>();
	private readonly connections = new Set<KitServerConnection>();
	private disposed = false;
	private disposePromise: Promise<void> | null = null;

	constructor(
		private readonly cwd: string,
		options: KitServerOptions = {},
	) {
		this.storage = options.storage ?? defaultStorage;
		this.startSession = options.startSession ?? startSession;
	}

	async listSessions(cwd?: string): Promise<SessionSummary[]> {
		this.assertActive();
		return cwd === undefined
			? this.storage.listAllSessions()
			: this.storage.listSessionsForCwd(cwd);
	}

	async createSession(cwd = this.cwd): Promise<SessionSummary> {
		this.assertActive();
		return this.trackMutation(async () => {
			const session = await this.storage.createSession(cwd);
			await this.storage.writeSession(session);
			return toSummary(session);
		});
	}

	async connectSession(sessionId: string): Promise<KitServerConnection> {
		this.assertActive();
		const running = await this.getOrStartSession(sessionId);
		this.assertActive();
		const connection = new KitServerConnection(sessionId, running, (value) =>
			this.connections.delete(value),
		);
		this.connections.add(connection);
		return connection;
	}

	dispose(): Promise<void> {
		if (this.disposePromise) return this.disposePromise;
		this.disposed = true;
		this.disposePromise = this.performDispose();
		return this.disposePromise;
	}

	private async performDispose(): Promise<void> {
		this.startupAbort.abort(new Error("Kit server is shutting down"));
		for (const connection of [...this.connections]) connection.close();
		const startupOperations = [...this.starting.values()];
		await Promise.allSettled(this.mutations);
		const startupResults = await Promise.allSettled(startupOperations);
		const sessionResults = await Promise.allSettled(
			[...this.sessions.values()].map((session) => session.dispose()),
		);
		this.sessions.clear();
		const failures = [...startupResults, ...sessionResults].flatMap((result) =>
			result.status === "rejected" &&
			!(result.reason instanceof SessionStartupCancelled)
				? [result.reason]
				: [],
		);
		if (failures.length === 1) throw failures[0];
		if (failures.length > 1) {
			throw new AggregateError(failures, "Kit server cleanup failed");
		}
	}

	private async getOrStartSession(sessionId: string): Promise<RunningSession> {
		const running = this.sessions.get(sessionId);
		if (running) return running;
		const pending = this.starting.get(sessionId);
		if (pending) return pending;
		const otherSessionId =
			this.sessions.keys().next().value ?? this.starting.keys().next().value;
		if (otherSessionId !== undefined) {
			throw new Error(
				"Multiple in-process sessions require session-scoped runtime globals",
			);
		}

		const operation = this.loadAndStartSession(sessionId);
		this.starting.set(sessionId, operation);
		try {
			return await operation;
		} finally {
			this.starting.delete(sessionId);
		}
	}

	private async loadAndStartSession(
		sessionId: string,
	): Promise<RunningSession> {
		const session = await this.storage.readSession(sessionId);
		if (!session) throw new Error(`Session not found: ${sessionId}`);
		if (this.disposed) throw new SessionStartupCancelled();
		let running: RunningSession;
		try {
			running = await this.startSession(session, this.startupAbort.signal);
		} catch (error) {
			if (this.disposed && error === this.startupAbort.signal.reason) {
				throw new SessionStartupCancelled();
			}
			throw error;
		}
		if (this.disposed) {
			await running.dispose();
			throw new SessionStartupCancelled();
		}
		this.sessions.set(sessionId, running);
		return running;
	}

	private trackMutation<T>(operation: () => Promise<T>): Promise<T> {
		const pending = operation();
		this.mutations.add(pending);
		const remove = () => this.mutations.delete(pending);
		pending.then(remove, remove);
		return pending;
	}

	private assertActive(): void {
		if (this.disposed) throw new Error("Kit server is disposed");
	}
}
