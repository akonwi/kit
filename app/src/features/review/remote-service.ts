import { createHash } from "node:crypto";
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

type ReviewRuntime = Pick<AgentRuntime, "getSession" | "subscribe">;

type RemoteReviewServiceOptions = {
	loadFiles?: (cwd: string) => Promise<ReviewFile[]>;
	getRepoRoot?: (cwd: string) => string;
};

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
	private readonly listeners = new Set<() => void>();
	private readonly loadFiles: (cwd: string) => Promise<ReviewFile[]>;
	private readonly resolveRepoRoot: (cwd: string) => string;
	private readonly unsubscribe: () => void;

	constructor(
		private readonly runtime: ReviewRuntime,
		options: RemoteReviewServiceOptions = {},
	) {
		this.sessionId = runtime.getSession().id;
		this.loadFiles = options.loadFiles ?? ((cwd) => loadReviewFiles(cwd));
		this.resolveRepoRoot = options.getRepoRoot ?? getRepoRoot;
		this.unsubscribe = runtime.subscribe((event: AgentRuntimeEvent) => {
			if (event.type !== "session.active.changed") return;
			const nextSessionId = runtime.getSession().id;
			if (nextSessionId === this.sessionId) return;
			this.sessionId = nextSessionId;
			this.files = [];
			this.fingerprint = "";
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
		if (sessionId !== this.runtime.getSession().id) {
			throw new Error("Active session changed; reload code review");
		}
	}

	private file(path: string): ReviewFile {
		const file = this.files.find((candidate) => candidate.path === path);
		if (!file) throw new Error("Review file is no longer present in the diff");
		return file;
	}

	async refresh(): Promise<RemoteReviewState> {
		const session = this.runtime.getSession();
		if (session.id !== this.sessionId) {
			this.sessionId = session.id;
			this.files = [];
			this.fingerprint = "";
			this.bump();
		}
		const files = await this.loadFiles(session.cwd);
		if (session.id !== this.runtime.getSession().id) {
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
}
