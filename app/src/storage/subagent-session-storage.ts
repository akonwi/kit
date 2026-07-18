import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import {
	appendFile,
	mkdir,
	readdir,
	readFile,
	rename,
	rm,
	writeFile,
} from "node:fs/promises";
import path from "node:path";
import { getKitPaths } from "../paths";
import {
	SESSION_VERSION,
	type SessionEntry,
	type SubagentEventSource,
	type SubagentSessionHeader,
} from "../session/types";
import type { AppendableSessionEntry } from "./session-storage";

export type CreateSubagentSessionOptions = {
	id: string;
	ownerSessionId: string;
	cwd: string;
	agentName: string;
	description?: string;
	model?: string;
	thinkingLevel?: SubagentSessionHeader["thinkingLevel"];
	source: SubagentEventSource;
};

type CachedSubagentSession = {
	header: SubagentSessionHeader;
	entries: SessionEntry[];
};

const cache = new Map<string, CachedSubagentSession>();
const writeChains = new Map<string, Promise<void>>();

export function subagentSessionsDir(): string {
	return path.join(getKitPaths().kitRoot, "sessions", "subagents");
}

export function subagentSessionPath(id: string): string {
	return path.join(subagentSessionsDir(), `${id}.jsonl`);
}

async function load(id: string): Promise<CachedSubagentSession | null> {
	const cached = cache.get(id);
	if (cached) return cached;
	const filePath = subagentSessionPath(id);
	if (!existsSync(filePath)) return null;
	try {
		const lines = (await readFile(filePath, "utf8"))
			.split("\n")
			.filter((line) => line.trim().length > 0);
		const headerLine = lines.shift();
		if (!headerLine) return null;
		const rawHeader = JSON.parse(headerLine);
		if (rawHeader?.type !== "session" || rawHeader.kind !== "subagent") {
			return null;
		}
		const entries: SessionEntry[] = [];
		for (const line of lines) {
			try {
				entries.push(JSON.parse(line) as SessionEntry);
			} catch {
				// Ignore malformed entries, matching primary session recovery.
			}
		}
		const state = {
			header: rawHeader as SubagentSessionHeader,
			entries,
		};
		cache.set(id, state);
		return state;
	} catch {
		return null;
	}
}

export async function createSubagentSession(
	options: CreateSubagentSessionOptions,
): Promise<void> {
	const filePath = subagentSessionPath(options.id);
	const existing = await load(options.id);
	if (existing) return;
	if (existsSync(filePath)) {
		throw new Error(`Sub-agent session is unreadable: ${options.id}`);
	}
	const timestamp = new Date().toISOString();
	const header: SubagentSessionHeader = {
		type: "session",
		version: SESSION_VERSION,
		kind: "subagent",
		id: options.id,
		createdAt: timestamp,
		cwd: options.cwd,
		ownerSessionId: options.ownerSessionId,
		agentName: options.agentName,
		description: options.description,
		source: options.source,
		model: options.model,
		thinkingLevel: options.thinkingLevel,
	};
	await mkdir(subagentSessionsDir(), { recursive: true });
	const temporaryPath = `${filePath}.${randomUUID()}.tmp`;
	await writeFile(temporaryPath, `${JSON.stringify(header)}\n`, "utf8");
	try {
		if (existsSync(filePath)) {
			const raced = await load(options.id);
			if (!raced)
				throw new Error(`Sub-agent session is unreadable: ${options.id}`);
			return;
		}
		await rename(temporaryPath, filePath);
	} finally {
		await rm(temporaryPath, { force: true });
	}
	cache.set(options.id, { header, entries: [] });
}

export async function readSubagentSessionHeader(
	id: string,
): Promise<SubagentSessionHeader | null> {
	return (await load(id))?.header ?? null;
}

export async function readSubagentSessionEntries(
	id: string,
): Promise<SessionEntry[]> {
	return [...((await load(id))?.entries ?? [])];
}

export async function appendSubagentSessionEntries(
	id: string,
	entries: AppendableSessionEntry[],
): Promise<SessionEntry[]> {
	let result: SessionEntry[] = [];
	const previous = writeChains.get(id) ?? Promise.resolve();
	const next = previous.then(async () => {
		const state = await load(id);
		if (!state) throw new Error(`Sub-agent session not found: ${id}`);
		let parentId = state.entries.at(-1)?.id ?? null;
		result = entries.map((entry) => {
			const prepared = {
				...entry,
				id: randomUUID(),
				parentId,
			} as SessionEntry;
			parentId = prepared.id;
			return prepared;
		});
		if (result.length === 0) return;
		await appendFile(
			subagentSessionPath(id),
			`\n${result.map((entry) => JSON.stringify(entry)).join("\n")}\n`,
			"utf8",
		);
		state.entries.push(...result);
	});
	writeChains.set(
		id,
		next.then(
			() => {},
			() => {},
		),
	);
	await next;
	return result;
}

export async function deleteSubagentSession(id: string): Promise<void> {
	await writeChains.get(id);
	await rm(subagentSessionPath(id), { force: true });
	cache.delete(id);
	writeChains.delete(id);
}

export async function deleteSubagentSessionsForOwner(
	ownerSessionId: string,
): Promise<void> {
	const dir = subagentSessionsDir();
	if (!existsSync(dir)) return;
	for (const file of await readdir(dir)) {
		if (!file.endsWith(".jsonl")) continue;
		const id = file.replace(/\.jsonl$/, "");
		const header = await readSubagentSessionHeader(id);
		if (header?.ownerSessionId === ownerSessionId) {
			await deleteSubagentSession(id);
		}
	}
}
