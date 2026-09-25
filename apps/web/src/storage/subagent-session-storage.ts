import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import { appendFile, mkdir, readdir, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { getKitPaths } from "../paths";
import {
	SESSION_VERSION,
	type SessionEntry,
	type SubagentEventSource,
	type SubagentSessionHeader,
} from "../session/types";
import { replaceFileAtomically, withFileLock } from "./atomic-file";
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

async function readUncached(id: string): Promise<CachedSubagentSession | null> {
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
		return {
			header: rawHeader as SubagentSessionHeader,
			entries,
		};
	} catch {
		return null;
	}
}

async function load(id: string): Promise<CachedSubagentSession | null> {
	const cached = cache.get(id);
	if (cached) return cached;
	const state = await readUncached(id);
	if (state) cache.set(id, state);
	return state;
}

function applyCachedState(
	target: CachedSubagentSession,
	replacement: CachedSubagentSession,
): void {
	target.header = replacement.header;
	target.entries = replacement.entries;
}

export async function createSubagentSession(
	options: CreateSubagentSessionOptions,
): Promise<void> {
	const filePath = subagentSessionPath(options.id);
	await mkdir(subagentSessionsDir(), { recursive: true });
	await withFileLock(filePath, async () => {
		const existing = await readUncached(options.id);
		if (existing) {
			const cached = cache.get(options.id);
			if (cached) applyCachedState(cached, existing);
			else cache.set(options.id, existing);
			return;
		}
		if (existsSync(filePath)) {
			throw new Error(`Sub-agent session is unreadable: ${options.id}`);
		}
		const header: SubagentSessionHeader = {
			type: "session",
			version: SESSION_VERSION,
			kind: "subagent",
			id: options.id,
			createdAt: new Date().toISOString(),
			cwd: options.cwd,
			ownerSessionId: options.ownerSessionId,
			agentName: options.agentName,
			description: options.description,
			source: options.source,
			model: options.model,
			thinkingLevel: options.thinkingLevel,
		};
		await replaceFileAtomically(filePath, `${JSON.stringify(header)}\n`);
		cache.set(options.id, { header, entries: [] });
	});
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
	const filePath = subagentSessionPath(id);
	const next = previous.then(async () => {
		await mkdir(subagentSessionsDir(), { recursive: true });
		return withFileLock(filePath, async () => {
			const refreshed = await readUncached(id);
			if (!refreshed) throw new Error(`Sub-agent session not found: ${id}`);
			const state = cache.get(id) ?? refreshed;
			if (state !== refreshed) applyCachedState(state, refreshed);
			cache.set(id, state);
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

			const compactionIndex = result.findLastIndex(
				(entry) => entry.type === "subagent_compaction",
			);
			if (compactionIndex >= 0) {
				parentId = null;
				result = result.slice(compactionIndex).map((entry) => {
					const reparented = { ...entry, parentId } as SessionEntry;
					parentId = reparented.id;
					return reparented;
				});
				const content = [state.header, ...result]
					.map((entry) => JSON.stringify(entry))
					.join("\n")
					.concat("\n");
				await replaceFileAtomically(filePath, content);
				state.entries = result;
				return;
			}

			await appendFile(
				subagentSessionPath(id),
				`\n${result.map((entry) => JSON.stringify(entry)).join("\n")}\n`,
				"utf8",
			);
			state.entries.push(...result);
		});
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
	await writeChains.get(id)?.catch(() => {});
	const filePath = subagentSessionPath(id);
	await mkdir(subagentSessionsDir(), { recursive: true });
	await withFileLock(filePath, async () => {
		await rm(filePath, { force: true });
		cache.delete(id);
	});
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
