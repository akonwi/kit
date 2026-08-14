import { createHash } from "node:crypto";
import type { CodeReviewMessagePart } from "../../messages/parts";
import type {
	AgentRuntime,
	AgentRuntimeEvent,
} from "../../runtime/agent-runtime";
import { getRepoRoot, loadReviewFiles, type ReviewFile } from "./model";

export type RemoteReviewFileSummary = {
	id: string;
	path: string;
	prevPath?: string;
	status: ReviewFile["status"];
	source: ReviewFile["source"];
	additions: number;
	deletions: number;
	changeCount: number;
};

export type RemoteReviewState = {
	sessionId: string;
	generation: number;
	repoRoot: string;
	files: RemoteReviewFileSummary[];
};

export type RemoteReviewFile = {
	sessionId: string;
	generation: number;
	file: RemoteReviewFileSummary & {
		rawPatch: string;
		hunks: ReviewFile["hunks"];
	};
};

export type RemoteReviewNote = {
	path: string;
	side: "additions" | "deletions";
	startLine: number;
	endLine: number;
	comment: string;
};

export type PreparedRemoteReviewSubmission = {
	submissionId: string;
	fingerprint: string;
	part: CodeReviewMessagePart | null;
};

type ReviewRuntime = Pick<
	AgentRuntime,
	"getMessages" | "getSession" | "subscribe"
>;

type RemoteReviewServiceOptions = {
	loadFiles?: (cwd: string) => Promise<ReviewFile[]>;
	getRepoRoot?: (cwd: string) => string;
};

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function fileCounts(file: ReviewFile): {
	additions: number;
	deletions: number;
} {
	let additions = 0;
	let deletions = 0;
	for (const hunk of file.hunks) {
		for (const line of hunk.lines) {
			if (line.kind === "add") additions += 1;
			else if (line.kind === "delete") deletions += 1;
		}
	}
	return { additions, deletions };
}

function filesFingerprint(files: ReviewFile[]): string {
	const hash = createHash("sha256");
	for (const file of files) {
		hash.update(file.id);
		hash.update("\0");
		hash.update(file.rawPatch);
		hash.update("\0");
	}
	return hash.digest("hex");
}

export class RemoteReviewService {
	private files: ReviewFile[] = [];
	private fingerprint = "";
	private generation = 0;
	private sessionId: string;
	private cwd: string;
	private readonly listeners = new Set<() => void>();
	private readonly acceptedSubmissions = new Map<string, string>();
	private readonly loadFiles: (cwd: string) => Promise<ReviewFile[]>;
	private readonly resolveRepoRoot: (cwd: string) => string;
	private readonly unsubscribe: () => void;

	constructor(
		private readonly runtime: ReviewRuntime,
		options: RemoteReviewServiceOptions = {},
	) {
		this.sessionId = runtime.getSession().id;
		this.cwd = runtime.getSession().cwd;
		this.loadFiles = options.loadFiles ?? ((cwd) => loadReviewFiles(cwd));
		this.resolveRepoRoot = options.getRepoRoot ?? getRepoRoot;
		this.unsubscribe = runtime.subscribe((event: AgentRuntimeEvent) => {
			if (event.type !== "session.active.changed") return;
			const session = runtime.getSession();
			if (session.id === this.sessionId && session.cwd === this.cwd) return;
			this.sessionId = session.id;
			this.cwd = session.cwd;
			this.files = [];
			this.fingerprint = "";
			this.acceptedSubmissions.clear();
			this.bump();
		});
	}

	dispose(): void {
		this.unsubscribe();
		this.listeners.clear();
	}

	subscribe(listener: () => void): () => void {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	}

	private bump(): void {
		this.generation += 1;
		for (const listener of this.listeners) listener();
	}

	private assertSession(sessionId: string): void {
		const session = this.runtime.getSession();
		if (sessionId !== session.id || session.cwd !== this.cwd) {
			throw new Error("Active session changed; reload code review");
		}
	}

	assertCurrent(sessionId: string, generation: number): void {
		this.assertSession(sessionId);
		if (generation !== this.generation) {
			throw new Error("Code review changed; reload before submitting");
		}
	}

	private file(path: string): ReviewFile {
		const file = this.files.find((candidate) => candidate.path === path);
		if (!file) throw new Error("Review file is no longer present in the diff");
		return file;
	}

	async refresh(): Promise<RemoteReviewState> {
		const session = this.runtime.getSession();
		if (session.id !== this.sessionId || session.cwd !== this.cwd) {
			this.sessionId = session.id;
			this.cwd = session.cwd;
			this.files = [];
			this.fingerprint = "";
			this.acceptedSubmissions.clear();
			this.bump();
		}
		const sessionId = session.id;
		const cwd = session.cwd;
		const files = await this.loadFiles(cwd);
		const currentSession = this.runtime.getSession();
		if (sessionId !== currentSession.id || cwd !== currentSession.cwd) {
			throw new Error("Active session changed; reload code review");
		}
		const fingerprint = filesFingerprint(files);
		if (fingerprint !== this.fingerprint) {
			this.files = files;
			this.fingerprint = fingerprint;
			this.bump();
		}
		return this.state();
	}

	private state(): RemoteReviewState {
		const session = this.runtime.getSession();
		return {
			sessionId: session.id,
			generation: this.generation,
			repoRoot: this.resolveRepoRoot(session.cwd),
			files: this.files.map((file) => ({
				id: file.id,
				path: file.path,
				...(file.prevPath ? { prevPath: file.prevPath } : {}),
				status: file.status,
				source: file.source,
				...fileCounts(file),
				changeCount: file.changeCount,
			})),
		};
	}

	getFile(sessionId: string, path: string): RemoteReviewFile {
		this.assertSession(sessionId);
		const file = this.file(path);
		return {
			sessionId,
			generation: this.generation,
			file: {
				id: file.id,
				path: file.path,
				...(file.prevPath ? { prevPath: file.prevPath } : {}),
				status: file.status,
				source: file.source,
				...fileCounts(file),
				changeCount: file.changeCount,
				rawPatch: file.rawPatch,
				hunks: file.hunks,
			},
		};
	}

	async prepareSubmission(
		submissionId: string,
		sessionId: string,
		generation: number,
		notes: RemoteReviewNote[],
	): Promise<PreparedRemoteReviewSubmission> {
		this.assertSession(sessionId);
		const fingerprint = createHash("sha256")
			.update(JSON.stringify({ sessionId, generation, notes }))
			.digest("hex");
		const acceptedFingerprint =
			this.acceptedSubmissions.get(submissionId) ??
			this.persistedSubmissionFingerprint(submissionId);
		if (acceptedFingerprint) {
			if (acceptedFingerprint !== fingerprint) {
				throw new Error("Review submission ID was reused with different notes");
			}
			return { submissionId, fingerprint, part: null };
		}
		await this.refresh();
		this.assertCurrent(sessionId, generation);

		const notesByPath = new Map<string, RemoteReviewNote[]>();
		for (const note of notes) {
			const file = this.file(note.path);
			if (!this.rangeExists(file, note)) {
				throw new Error(
					`Review range is no longer present in ${note.path}; reload before submitting`,
				);
			}
			const existing = notesByPath.get(note.path);
			if (existing) existing.push(note);
			else notesByPath.set(note.path, [note]);
		}

		return {
			submissionId,
			fingerprint,
			part: {
				type: "code-review",
				review: {
					submittedAt: new Date().toISOString(),
					submissionId,
					submissionFingerprint: fingerprint,
					files: this.files.flatMap((file) => {
						const fileNotes = notesByPath.get(file.path);
						if (!fileNotes) return [];
						return [
							{
								path: file.path,
								fileComment: "",
								ranges: fileNotes.map((note) => ({
									side: note.side,
									startLine: note.startLine,
									endLine: note.endLine,
									comment: note.comment,
								})),
							},
						];
					}),
				},
			},
		};
	}

	private persistedSubmissionFingerprint(
		submissionId: string,
	): string | undefined {
		for (const messageValue of this.runtime.getMessages()) {
			const message: unknown = messageValue;
			if (!isRecord(message) || !Array.isArray(message.content)) continue;
			for (const part of message.content) {
				if (!isRecord(part) || part.type !== "code-review") continue;
				const review = part.review;
				if (
					!isRecord(review) ||
					review.submissionId !== submissionId ||
					typeof review.submissionFingerprint !== "string"
				) {
					continue;
				}
				return review.submissionFingerprint;
			}
		}
		return undefined;
	}

	markSubmissionAccepted(submission: PreparedRemoteReviewSubmission): void {
		this.acceptedSubmissions.set(
			submission.submissionId,
			submission.fingerprint,
		);
		while (this.acceptedSubmissions.size > 256) {
			const oldest = this.acceptedSubmissions.keys().next().value;
			if (typeof oldest !== "string") break;
			this.acceptedSubmissions.delete(oldest);
		}
	}

	private rangeExists(file: ReviewFile, note: RemoteReviewNote): boolean {
		const requiredLines = note.endLine - note.startLine + 1;
		if (requiredLines <= 0) return false;
		const lines = new Set<number>();
		for (const hunk of file.hunks) {
			for (const line of hunk.lines) {
				const lineNumber =
					note.side === "additions"
						? line.additionLineNumber
						: line.deletionLineNumber;
				if (
					lineNumber !== undefined &&
					lineNumber >= note.startLine &&
					lineNumber <= note.endLine
				) {
					lines.add(lineNumber);
				}
			}
		}
		return lines.size === requiredLines;
	}
}
